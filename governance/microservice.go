package governance

import (
	"encoding/json"
	"fmt"
	"github.com/golang/glog"
	"github.com/open-horizon/anax/config"
	"github.com/open-horizon/anax/events"
	"github.com/open-horizon/anax/exchange"
	"github.com/open-horizon/anax/microservice"
	"github.com/open-horizon/anax/persistence"
	"github.com/open-horizon/anax/policy"
	"github.com/open-horizon/anax/producer"
	"net/url"
)

func (w *GovernanceWorker) governMicroservices() int {

	// handle microservice upgrade and rollback. The upgrade includes inactive upgrades if the associated agreements happen to be 0.
	glog.V(4).Infof(logString(fmt.Sprintf("governing microservice upgrades")))
	if ms_defs, err := persistence.FindMicroserviceDefs(w.db, []persistence.MSFilter{persistence.UnarchivedMSFilter()}); err != nil {
		glog.Errorf(logString(fmt.Sprintf("Error getting microservice definitions from db. %v", err)))
	} else if ms_defs != nil && len(ms_defs) > 0 {
		for _, ms := range ms_defs {
			// check if ms is ready for rollback for those execution did not start within certian time
			if microservice.MicroserviceNeedsRollback(&ms) {
				if old_msdef, err := microservice.GetRollbackMicroserviceDef(&ms, w.db); err != nil {
					glog.Errorf(logString(fmt.Sprintf("Error finding the old microservice definition to rollback to for %v version %v key %v. %v", ms.SpecRef, ms.Version, ms.Id, err)))
				} else if old_msdef == nil {
					glog.Errorf(logString(fmt.Sprintf("Unable to find the old microservice definition to rollback to for %v version %v key %v. %v", ms.SpecRef, ms.Version, ms.Id, err)))
				} else if err := w.RollbackMicroservice(old_msdef, &ms); err != nil {
					glog.Errorf(logString(fmt.Sprintf("Failed to rollback %v from version %v key %v to version %v key %v. %v", ms.SpecRef, ms.Version, ms.Id, old_msdef.Version, old_msdef.Id, err)))
				}
			} else {
				// upgrade the microserice if needed
				w.HandleMicroserviceUpgrade(ms.Id, false)
			}
		}
	}

	// handle microservice instance containers down and
	glog.V(4).Infof(logString(fmt.Sprintf("governing microservice containers")))
	if ms_instances, err := persistence.FindMicroserviceInstances(w.db, []persistence.MIFilter{persistence.AllMIFilter(), persistence.UnarchivedMIFilter()}); err != nil {
		glog.Errorf(logString(fmt.Sprintf("Error retrieving all microservice instances from database, error: %v", err)))
	} else if ms_instances != nil {
		for _, msi := range ms_instances {
			// only check the ones that has containers started already and not in the middle of cleanup
			if hasWL, _ := msi.HasWorkload(w.db); hasWL && msi.ExecutionStartTime != 0 && msi.CleanupStartTime == 0 {
				glog.V(3).Infof(logString(fmt.Sprintf("fire event to ensure microservice containers are still up for microservice instance %v.", msi.GetKey())))

				// ensure containers are still running
				w.Messages() <- events.NewMicroserviceMaintenanceMessage(events.CONTAINER_MAINTAIN, msi.GetKey())
			}
		}
	}
	return 0
}

// create microservice instance and loads the containers for the given microservice def
func (w *GovernanceWorker) StartMicroservice(ms_key string) (*persistence.MicroserviceInstance, error) {
	glog.V(5).Infof(logString(fmt.Sprintf("Starting microservice instance for %v", ms_key)))
	if msdef, err := persistence.FindMicroserviceDefWithKey(w.db, ms_key); err != nil {
		return nil, fmt.Errorf(logString(fmt.Sprintf("Error finding microserivce definition from db with key %v. %v", ms_key, err)))
	} else if msdef == nil {
		return nil, fmt.Errorf(logString(fmt.Sprintf("No microserivce definition available for key %v.", ms_key)))
	} else {
		wls := msdef.Workloads
		if wls == nil || len(wls) == 0 {
			glog.Infof(logString(fmt.Sprintf("No workload needed for microservice %v.", msdef.SpecRef)))
			if mi, err := persistence.NewMicroserviceInstance(w.db, msdef.SpecRef, msdef.Version, ms_key); err != nil {
				return nil, fmt.Errorf(logString(fmt.Sprintf("Error persisting microservice instance for %v %v %v.", msdef.SpecRef, msdef.Version, ms_key)))
				// if the new microservice does not have containers, just mark it done.
			} else if mi, err := persistence.UpdateMSInstanceExecutionState(w.db, mi.GetKey(), true, 0, ""); err != nil {
				return nil, fmt.Errorf(logString(fmt.Sprintf("Failed to update the ExecutionStartTime for microservice instance %v. %v", mi.GetKey(), err)))
			} else {
				return mi, nil
			}
		}

		for _, wl := range wls {
			// convert the torrent string to a structure
			var torrent policy.Torrent
			if err := json.Unmarshal([]byte(wl.Torrent), &torrent); err != nil {
				return nil, fmt.Errorf(logString(fmt.Sprintf("The torrent definition for microservice %v has error: %v", msdef.SpecRef, err)))
			}

			// convert workload to policy workload structure
			var ms_workload policy.Workload
			ms_workload.Deployment = wl.Deployment
			ms_workload.DeploymentSignature = wl.DeploymentSignature
			ms_workload.Torrent = torrent
			ms_workload.WorkloadPassword = ""
			ms_workload.DeploymentUserInfo = ""

			// verify torrent url
			if url, err := url.Parse(torrent.Url); err != nil {
				return nil, fmt.Errorf("ill-formed URL: %v, error %v", torrent.Url, err)
			} else {

				// Verify the deployment signature
				if pemFiles, err := w.Config.Collaborators.KeyFileNamesFetcher.GetKeyFileNames(w.Config.Edge.PublicKeyPath, w.Config.UserPublicKeyPath()); err != nil {
					return nil, fmt.Errorf(logString(fmt.Sprintf("received error getting pem key files: %v", err)))
				} else if err := ms_workload.HasValidSignature(pemFiles); err != nil {
					return nil, fmt.Errorf(logString(fmt.Sprintf("microservice container has invalid deployment signature %v for %v", ms_workload.DeploymentSignature, ms_workload.Deployment)))
				}

				// save the instance
				if ms_instance, err := persistence.NewMicroserviceInstance(w.db, msdef.SpecRef, msdef.Version, ms_key); err != nil {
					return nil, fmt.Errorf(logString(fmt.Sprintf("Error persisting microservice instance for %v %v %v.", msdef.SpecRef, msdef.Version, ms_key)))
				} else {
					// Fire an event to the torrent worker so that it will download the container
					cc := events.NewContainerConfig(*url, ms_workload.Torrent.Signature, ms_workload.Deployment, ms_workload.DeploymentSignature, ms_workload.DeploymentUserInfo, "")

					// convert the user input from the service attributes to env variables
					if attrs, err := persistence.FindApplicableAttributes(w.db, msdef.SpecRef); err != nil {
						return nil, fmt.Errorf(logString(fmt.Sprintf("Unable to fetch microservice preferences for %v. Err: %v", msdef.SpecRef, err)))
					} else if envAdds, err := persistence.AttributesToEnvvarMap(attrs, config.ENVVAR_PREFIX); err != nil {
						return nil, fmt.Errorf(logString(fmt.Sprintf("Failed to convert microservice preferences to environmental variables for %v. Err: %v", msdef.SpecRef, err)))
					} else {
						envAdds[config.ENVVAR_PREFIX+"DEVICE_ID"] = exchange.GetId(w.deviceId)
						envAdds[config.ENVVAR_PREFIX+"ORGANIZATION"] = exchange.GetOrg(w.deviceId)
						envAdds[config.ENVVAR_PREFIX+"EXCHANGE_URL"] = w.Config.Edge.ExchangeURL
						// Add in any default variables from the microservice userInputs that havent been overridden
						for _, ui := range msdef.UserInputs {
							if ui.DefaultValue != "" {
								if _, ok := envAdds[ui.Name]; !ok {
									envAdds[ui.Name] = ui.DefaultValue
								}
							}
						}
						lc := events.NewContainerLaunchContext(cc, &envAdds, events.BlockchainConfig{}, ms_instance.GetKey())
						w.Messages() <- events.NewLoadContainerMessage(events.LOAD_CONTAINER, lc)
					}

					return ms_instance, nil // assume there is only one workload for a microservice
				}
			}
		}
	}

	return nil, nil
}

// cleans the microservice instance with its associated agreements
func (w *GovernanceWorker) CleanupMicroservice(spec_ref string, version string, inst_key string, ms_reason_code uint) error {
	glog.V(5).Infof(logString(fmt.Sprintf("Deleting microservice instance %v", inst_key)))

	// archive this microservice instance in the db
	if ms_inst, err := persistence.MicroserviceInstanceCleanupStarted(w.db, inst_key); err != nil {
		glog.Errorf(logString(fmt.Sprintf("Error setting cleanup start time for microservice instance %v. %v", inst_key, err)))
		return fmt.Errorf(logString(fmt.Sprintf("Error setting cleanup start time for microservice instance %v. %v", inst_key, err)))
	} else if ms_inst == nil {
		glog.Errorf(logString(fmt.Sprintf("Unable to find microservice instance %v.", inst_key)))
		return fmt.Errorf(logString(fmt.Sprintf("Unable to find microservice instance %v.", inst_key)))
		// remove all the containers for agreements associated with it so that new agreements can be created over the new microservice
	} else if agreements, err := w.FindEstablishedAgreementsWithIds(ms_inst.AssociatedAgreements); err != nil {
		glog.Errorf(logString(fmt.Sprintf("Error finding agreements %v from the db. %v", ms_inst.AssociatedAgreements, err)))
		return fmt.Errorf(logString(fmt.Sprintf("Error finding agreements %v from the db. %v", ms_inst.AssociatedAgreements, err)))
	} else if agreements != nil {
		// If this function is called by the only clean up the workload containers for the agreement
		glog.V(5).Infof(logString(fmt.Sprintf("Removing all the containers for associated agreements %v", ms_inst.AssociatedAgreements)))
		for _, ag := range agreements {
			// send the event to the container so that the workloads can be deleted
			w.Messages() <- events.NewGovernanceWorkloadCancelationMessage(events.AGREEMENT_ENDED, events.AG_TERMINATED, ag.AgreementProtocol, ag.CurrentAgreementId, ag.CurrentDeployment)

			var ag_reason_code uint
			var ag_reason_text string
			switch ms_reason_code {
			case microservice.MS_IMAGE_LOAD_FAILED:
				ag_reason_code = w.producerPH[ag.AgreementProtocol].GetTerminationCode(producer.TERM_REASON_MS_IMAGE_LOAD_FAILURE)
				ag_reason_text = w.producerPH[ag.AgreementProtocol].GetTerminationReason(ag_reason_code)
			case microservice.MS_DELETED_BY_UPGRADE_PROCESS:
				ag_reason_code = w.producerPH[ag.AgreementProtocol].GetTerminationCode(producer.TERM_REASON_MS_UPGRADE_REQUIRED)
				ag_reason_text = w.producerPH[ag.AgreementProtocol].GetTerminationReason(ag_reason_code)
			default:
				ag_reason_code = w.producerPH[ag.AgreementProtocol].GetTerminationCode(producer.TERM_REASON_MICROSERVICE_FAILURE)
				ag_reason_text = w.producerPH[ag.AgreementProtocol].GetTerminationReason(ag_reason_code)
			}

			// end the agreements
			glog.V(3).Infof("Ending the agreement: %v because microservice %v is deleted", ag.CurrentAgreementId, inst_key)
			w.cancelAgreement(ag.CurrentAgreementId, ag.AgreementProtocol, ag_reason_code, ag_reason_text)
		}

		// remove all the microservice containers if any
		if has_wl, err := ms_inst.HasWorkload(w.db); err != nil {
			return fmt.Errorf(logString(fmt.Sprintf("Error checking if the microservice %v has workload. %v", ms_inst.GetKey(), err)))
		} else if has_wl {
			glog.V(5).Infof(logString(fmt.Sprintf("Removing all the containers for %v", inst_key)))
			w.Messages() <- events.NewMicroserviceCancellationMessage(events.CANCEL_MICROSERVICE, inst_key)
		}
	}

	// archive this microservice instance
	if _, err := persistence.ArchiveMicroserviceInstance(w.db, inst_key); err != nil {
		glog.Errorf(logString(fmt.Sprintf("Error archiving microservice instance %v. %v", inst_key, err)))
		return fmt.Errorf(logString(fmt.Sprintf("Error archiving microservice instance %v. %v", inst_key, err)))
	}

	return nil
}

// Upgrade the given microservice, assuming the given microservice is ready for a upgrade.
// One can check it by calling microservice.MicroserviceReadyForUpgrade to find out.
func (w *GovernanceWorker) UpgradeMicroservice(msdef *persistence.MicroserviceDefinition, new_msdef *persistence.MicroserviceDefinition) error {
	glog.V(3).Infof(logString(fmt.Sprintf("Start upgrading microservice %v from version %v to version %v", msdef.SpecRef, msdef.Version, new_msdef.Version)))

	// archive the old ms def and save the new one to db
	if _, err := persistence.MsDefArchived(w.db, msdef.Id); err != nil {
		return fmt.Errorf(logString(fmt.Sprintf("Failed to archived microservice definition %v. %v", msdef, err)))
	} else if err := persistence.SaveOrUpdateMicroserviceDef(w.db, new_msdef); err != nil {
		return fmt.Errorf(logString(fmt.Sprintf("Failed to save microservice definition to db. %v. %v", new_msdef, err)))
	} else if _, err := persistence.MSDefUpgradeNewMsId(w.db, msdef.Id, new_msdef.Id); err != nil {
		return fmt.Errorf(logString(fmt.Sprintf("Failed to update the UpgradeNewMsId to %v for microservice def %v version %v id %v. %v", new_msdef.Id, msdef.SpecRef, msdef.Version, msdef.Id, err)))
	} else if _, err := persistence.MSDefUpgradeStarted(w.db, new_msdef.Id); err != nil {
		return fmt.Errorf(logString(fmt.Sprintf("Failed to update the UpgradeStartTime for microservice def %v version %v id %v. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
	}

	// unregister the old ms from exchange
	var unregError error
	unregError = nil
	unregError = microservice.RemoveMicroservicePolicy(msdef.SpecRef, msdef.Org, msdef.Version, msdef.Id, w.Config.Edge.PolicyPath, w.pm)
	if unregError != nil {
		glog.Errorf(logString(fmt.Sprintf("Failed to remove microservice policy for microservice def %v version %v. %v", msdef.SpecRef, msdef.Version, unregError)))
	} else if unregError = microservice.UnregisterMicroserviceExchange(msdef.SpecRef, w.Config.Collaborators.HTTPClientFactory, w.Config.Edge.ExchangeURL, w.deviceId, w.deviceToken, w.db); unregError != nil {
		glog.Errorf(logString(fmt.Sprintf("Failed to unregister microservice from the exchange for microservice def %v. %v", msdef.SpecRef, unregError)))
	}
	// update msdef UpgradeMsUnregisteredTime
	if unregError != nil {
		if _, err := persistence.MSDefUpgradeFailed(w.db, new_msdef.Id, microservice.MS_UNREG_EXCH_FAILED, microservice.DecodeReasonCode(microservice.MS_UNREG_EXCH_FAILED)); err != nil {
			return fmt.Errorf(logString(fmt.Sprintf("Failed to update microservice upgrading failure reason for microservice def %v version %v id %v. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
		}
	} else {
		if _, err := persistence.MSDefUpgradeMsUnregistered(w.db, new_msdef.Id); err != nil {
			return fmt.Errorf(logString(fmt.Sprintf("Failed to update the UpgradeMsUnregisteredTime for microservice def %v version %v id %v. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
		}
	}

	// clean up old microservice
	var eClearError error
	var ms_insts []persistence.MicroserviceInstance
	eClearError = nil
	if ms_insts, eClearError = persistence.FindMicroserviceInstances(w.db, []persistence.MIFilter{persistence.AllInstancesMIFilter(msdef.SpecRef, msdef.Version), persistence.UnarchivedMIFilter()}); eClearError != nil {
		glog.Errorf(logString(fmt.Sprintf("Error retrieving all the microservice instaces from db for %v version %v key %v. %v", msdef.SpecRef, msdef.Version, msdef.Id, eClearError)))
	} else if ms_insts != nil && len(ms_insts) > 0 {
		for _, msi := range ms_insts {
			if msi.MicroserviceDefId == msdef.Id {
				if eClearError = w.CleanupMicroservice(msdef.SpecRef, msdef.Version, msi.GetKey(), microservice.MS_DELETED_BY_UPGRADE_PROCESS); eClearError != nil {
					glog.Errorf(logString(fmt.Sprintf("Error cleanup microservice instaces %v. %v", msi.GetKey(), eClearError)))
				}
			}
		}
	}
	// update msdef UpgradeAgreementsClearedTime
	if eClearError != nil {
		if _, err := persistence.MSDefUpgradeFailed(w.db, new_msdef.Id, microservice.MS_CLEAR_OLD_AGS_FAILED, microservice.DecodeReasonCode(microservice.MS_CLEAR_OLD_AGS_FAILED)); err != nil {
			return fmt.Errorf(logString(fmt.Sprintf("Failed to update the UpgradeAgreementsClearedTime for microservice def %v version %v id %v. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
		}
	} else {
		if _, err := persistence.MsDefUpgradeAgreementsCleared(w.db, new_msdef.Id); err != nil {
			return fmt.Errorf(logString(fmt.Sprintf("Failed to update the UpgradeAgreementsClearedTime for microservice def %v version %v id %v. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
		}
	}

	// start new microservice containers
	if _, err := w.StartMicroservice(new_msdef.Id); err != nil {
		return fmt.Errorf(logString(fmt.Sprintf("Error starting microservice instaces for microservice def %v version %v key %v. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
	}

	// if the new microservice does not have containers, just mark containers are up.
	if new_msdef.Workloads == nil || len(new_msdef.Workloads) == 0 {
		if _, err := persistence.MSDefUpgradeExecutionStarted(w.db, new_msdef.Id); err != nil {
			return fmt.Errorf(logString(fmt.Sprintf("Failed to update the MSDefUpgradeExecutionStarted for microservice def %v version %v id %v. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
		}

		// create a new policy file and register the new microservice in exchange
		if err := microservice.GenMicroservicePolicy(new_msdef, w.Config.Edge.PolicyPath, w.db, w.Messages(), exchange.GetOrg(w.deviceId)); err != nil {
			if _, err := persistence.MSDefUpgradeFailed(w.db, new_msdef.Id, microservice.MS_REREG_EXCH_FAILED, microservice.DecodeReasonCode(microservice.MS_REREG_EXCH_FAILED)); err != nil {
				return fmt.Errorf(logString(fmt.Sprintf("Failed to update microservice upgrading failure reason for microservice def %v version %v id %v. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
			}
		} else {
			if _, err := persistence.MSDefUpgradeMsReregistered(w.db, new_msdef.Id); err != nil {
				return fmt.Errorf(logString(fmt.Sprintf("Failed to update the UpgradeMsReregisteredTime for microservice def %v version %v id %v. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
			}
		}

		glog.V(3).Infof(logString(fmt.Sprintf("End upgrading microservice %v version %v key %v", msdef.SpecRef, msdef.Version, msdef.Id)))
	} else {
		glog.V(3).Infof(logString(fmt.Sprintf("End phase 1/2 of upgrading microservice %v version %v key %v", msdef.SpecRef, msdef.Version, msdef.Id)))
	}
	return nil
}

// Rollback the Microservice
func (w *GovernanceWorker) RollbackMicroservice(old_msdef *persistence.MicroserviceDefinition, new_msdef *persistence.MicroserviceDefinition) error {
	glog.V(3).Infof(logString(fmt.Sprintf("Start rolling back microservice %v from version %v to version %v", new_msdef.SpecRef, new_msdef.Version, old_msdef.Version)))
	glog.V(3).Infof(logString(fmt.Sprintf("Start rolling back microservice from %v to %v", new_msdef, old_msdef)))

	// archive the new ms def and unarchive the new one to db
	if _, err := persistence.MsDefArchived(w.db, new_msdef.Id); err != nil {
		return fmt.Errorf(logString(fmt.Sprintf("Failed to archived microservice definition %v. %v", new_msdef, err)))
	} else if _, err := persistence.MsDefUnarchived(w.db, old_msdef.Id); err != nil {
		return fmt.Errorf(logString(fmt.Sprintf("Failed to un-archived microservice definition %v. %v", old_msdef, err)))
	}

	// clean up new microservice
	if ms_insts, err := persistence.FindMicroserviceInstances(w.db, []persistence.MIFilter{persistence.AllInstancesMIFilter(new_msdef.SpecRef, new_msdef.Version), persistence.UnarchivedMIFilter()}); err != nil {
		glog.Errorf(logString(fmt.Sprintf("Error retrieving all the microservice instaces from db for %v version %v key %v. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
	} else if ms_insts != nil && len(ms_insts) > 0 {
		for _, msi := range ms_insts {
			if msi.MicroserviceDefId == new_msdef.Id {
				if err := w.CleanupMicroservice(new_msdef.SpecRef, new_msdef.Version, msi.GetKey(), microservice.MS_DELETED_BY_UPGRADE_PROCESS); err != nil {
					glog.Errorf(logString(fmt.Sprintf("Error cleanup microservice instaces %v. %v", msi.GetKey(), err)))
				}
			}
		}
	}

	// unregister the new ms from exchange
	if new_msdef.UpgradeMsReregisteredTime > 0 {
		if err := microservice.RemoveMicroservicePolicy(new_msdef.SpecRef, new_msdef.Org, new_msdef.Version, new_msdef.Id, w.Config.Edge.PolicyPath, w.pm); err != nil {
			return fmt.Errorf(logString(fmt.Sprintf("Failed to remove microservice policy for microservice def %v version %v key %d. %v", new_msdef.SpecRef, new_msdef.Version, new_msdef.Id, err)))
		} else if err := microservice.UnregisterMicroserviceExchange(new_msdef.SpecRef, w.Config.Collaborators.HTTPClientFactory, w.Config.Edge.ExchangeURL, w.deviceId, w.deviceToken, w.db); err != nil {
			return fmt.Errorf(logString(fmt.Sprintf("Failed to unregister microservice from the exchange for microservice def %v. %v", new_msdef.SpecRef, err)))
		}
	}

	// no need to start the old ms containers here becuase they will be started right before the workload containers are up

	// restore the old policy and fire an event to register the old policy
	if new_msdef.UpgradeMsUnregisteredTime > 0 {
		if fullFileName, err := microservice.RestoreMicroservicePolicyFile(old_msdef.SpecRef, old_msdef.Version, old_msdef.Id, w.Config.Edge.PolicyPath, w.pm); err != nil {
			return fmt.Errorf(logString(fmt.Sprintf("Error restoring old microservice policy for microservice def %v version %v key %v. %v", old_msdef.SpecRef, old_msdef.Version, old_msdef.Id, err)))
		} else {
			w.Messages() <- events.NewPolicyCreatedMessage(events.NEW_POLICY, fullFileName)
		}
	}

	glog.V(3).Infof(logString(fmt.Sprintf("End rolling back microservice %v from version %v to version %v", old_msdef.SpecRef, new_msdef.Version, old_msdef.Version)))

	return nil
}

// given a microservice id and check if it is set for upgrade, if yes do the upgrade
func (w *GovernanceWorker) HandleMicroserviceUpgrade(msdef_id string, inactiveOnly bool) {
	glog.V(3).Infof(logString(fmt.Sprintf("handling microserivce upgrade for microservice id %v", msdef_id)))
	if msdef, err := persistence.FindMicroserviceDefWithKey(w.db, msdef_id); err != nil {
		glog.Errorf(logString(fmt.Sprintf("error getting microservice definitions %v from db. %v", msdef_id, err)))
	} else if ((!inactiveOnly) || (!msdef.ActiveUpgrade)) && microservice.MicroserviceReadyForUpgrade(msdef, w.db) {
		// find the new ms def to upgrade to
		if new_msdef, err := microservice.GetUpgradeMicroserviceDef(msdef, w.Config.Collaborators.HTTPClientFactory, w.Config.Edge.ExchangeURL, w.deviceId, w.deviceToken, w.db); err != nil {
			glog.Errorf(logString(fmt.Sprintf("Error finding the new microservice definition to upgrade to for %v version %v. %v", msdef.SpecRef, msdef.Version, err)))
		} else if new_msdef == nil {
			glog.V(5).Infof(logString(fmt.Sprintf("No changes for microservice definition %v, no need to upgrade.", msdef.SpecRef)))
		} else if err := w.UpgradeMicroservice(msdef, new_msdef); err != nil {
			glog.Errorf(logString(fmt.Sprintf("Error upgrading microservice %v version %v key %v. %v", msdef.SpecRef, msdef.Version, msdef.Id, err)))
			// upgrade failed, will rollback
			if new_msdef1, err := persistence.FindMicroserviceDefWithKey(w.db, new_msdef.Id); err != nil {
				glog.Errorf(logString(fmt.Sprintf("error getting microservice definitions %v from db. %v", new_msdef.Id, err)))
			} else if err := w.RollbackMicroservice(msdef, new_msdef1); err != nil {
				glog.Errorf(logString(fmt.Sprintf("Failed to rollback %v from version %v key %v to version %v key %v. %v", new_msdef1.SpecRef, new_msdef1.Version, new_msdef1.Id, msdef.Version, msdef.Id, err)))
			}
		}
	}
}

// starts an microservice instance for the given agreement according to the microservice sharing mode
func (w *GovernanceWorker) startMicroserviceInstForAgreement(msdef *persistence.MicroserviceDefinition, agreementId string) error {
	glog.V(3).Infof(logString(fmt.Sprintf("start microserivce instance %v for agreement %v", msdef.SpecRef, agreementId)))

	var msi *persistence.MicroserviceInstance
	needs_new_ms := false

	// always start a new ms instance if the sharing mode is multiple
	if msdef.Sharable == exchange.MS_SHARING_MODE_MULTIPLE {
		needs_new_ms = true
		// for other sharing mode, start a new ms instance only if there is no existing one
		// the "exclusive" sharing mode is taken cared by the maxAgreements=1 so that there will no be more than one agreements to come at any time
	} else if ms_insts, err := persistence.FindMicroserviceInstances(w.db, []persistence.MIFilter{persistence.AllInstancesMIFilter(msdef.SpecRef, msdef.Version), persistence.UnarchivedMIFilter()}); err != nil {
		return fmt.Errorf(logString(fmt.Sprintf("Error retrieving all the microservice instaces from db for %v version %v key %v. %v", msdef.SpecRef, msdef.Version, msdef.Id, err)))
	} else if ms_insts == nil || len(ms_insts) == 0 {
		needs_new_ms = true
	} else {
		msi = &ms_insts[0]
	}

	if needs_new_ms {
		var inst_err error
		if msi, inst_err = w.StartMicroservice(msdef.Id); inst_err != nil {
			return fmt.Errorf(logString(fmt.Sprintf("Failed to start microservice instance for %v version %v key %v. %v", msdef.SpecRef, msdef.Version, msdef.Id, inst_err)))
		}
	}

	// add the agreement id into the msinstance so that the workload containers know which ms instance to associate with
	if _, err := persistence.UpdateMSInstanceAssociatedAgreements(w.db, msi.GetKey(), true, agreementId); err != nil {
		return fmt.Errorf(logString(fmt.Sprintf("error adding agreement id %v to the microservice %v: %v", agreementId, msi.GetKey(), err)))
	}

	return nil
}

// process microservice execution start or failure for  the upgrade process. Rollback if necessary.
func (w *GovernanceWorker) handleMicroserviceUpgradeExecStateChange(msdef *persistence.MicroserviceDefinition, msinst_key string, exec_started bool) {
	glog.V(3).Infof(logString(fmt.Sprintf("handle microservice instance execution status change for %v. Execution started: %v", msinst_key, exec_started)))

	needs_rollback := false

	if exec_started {
		// upgrade msdef UpgradeExecutionStartTime if it is an upgrade process
		if _, err := persistence.MSDefUpgradeExecutionStarted(w.db, msdef.Id); err != nil {
			glog.Errorf(logString(fmt.Sprintf("Failed to update the MSDefUpgradeExecutionStarted for microservice def %v version %v id %v. %v", msdef.SpecRef, msdef.Version, msdef.Id, err)))
			needs_rollback = true
		}

		// delete this instance if the sharing mode is "multiple" because a new one will be created for each new agreement
		if msdef.Sharable == exchange.MS_SHARING_MODE_MULTIPLE {
			if err := w.CleanupMicroservice(msdef.SpecRef, msdef.Version, msinst_key, microservice.MS_DELETED_BY_UPGRADE_PROCESS); err != nil {
				glog.Errorf(logString(fmt.Sprintf("Failed to delete the microservice instance %v. %v", msinst_key, err)))
				needs_rollback = true
			}
		}

		// finish up part2 of the upgrade process:
		// create a new policy file and register the new microservice in exchange
		if err := microservice.GenMicroservicePolicy(msdef, w.Config.Edge.PolicyPath, w.db, w.Messages(), exchange.GetOrg(w.deviceId)); err != nil {
			if _, err := persistence.MSDefUpgradeFailed(w.db, msdef.Id, microservice.MS_REREG_EXCH_FAILED, microservice.DecodeReasonCode(microservice.MS_REREG_EXCH_FAILED)); err != nil {
				glog.Errorf(logString(fmt.Sprintf("Failed to update microservice upgrading failure reason for microservice def %v version %v id %v. %v", msdef.SpecRef, msdef.Version, msdef.Id, err)))
				needs_rollback = true
			}
		} else {
			if _, err := persistence.MSDefUpgradeMsReregistered(w.db, msdef.Id); err != nil {
				glog.Errorf(logString(fmt.Sprintf("Failed to update the UpgradeMsReregisteredTime for microservice def %v version %v id %v. %v", msdef.SpecRef, msdef.Version, msdef.Id, err)))
				needs_rollback = true
			}
		}

		glog.V(3).Infof(logString(fmt.Sprintf("End phase 2/2 of upgrading microservice %v version %v key %v", msdef.SpecRef, msdef.Version, msdef.Id)))
	} else {
		// upgrade msdef with error info if it is a microservice upgrade process
		if _, err := persistence.MSDefUpgradeFailed(w.db, msdef.Id, microservice.MS_EXEC_FAILED, microservice.DecodeReasonCode(microservice.MS_EXEC_FAILED)); err != nil {
			glog.Errorf(logString(fmt.Sprintf("Failed to update the UpgradeAgreementsClearedTime for microservice def %v version %v id %v. %v", msdef.SpecRef, msdef.Version, msdef.Id, err)))
		}

		needs_rollback = true
	}

	if needs_rollback {
		// rollback to the old microservice
		if old_msdef, err := microservice.GetRollbackMicroserviceDef(msdef, w.db); err != nil {
			glog.Errorf(logString(fmt.Sprintf("Error finding the old microservice definition to rollback to for %v %v version key %v. error: %v", msdef.SpecRef, msdef.Version, msdef.Id, err)))
		} else if old_msdef == nil {
			glog.Errorf(logString(fmt.Sprintf("Unable to find the old microservice definition to rollback to for %v %v version key %v. error: %v", msdef.SpecRef, msdef.Version, msdef.Id, err)))
		} else if err := w.RollbackMicroservice(old_msdef, msdef); err != nil {
			glog.Errorf(logString(fmt.Sprintf("Failed to rollback %v from version %v key %v to version %v key %v. %v", msdef.SpecRef, msdef.Version, msdef.Id, old_msdef.Version, old_msdef.Id, err)))
		}
	}
}

// process microservice instance after an agreement is ended.
func (w *GovernanceWorker) handleMicroserviceInstForAgEnded(agreementId string, skipUpgrade bool) {
	glog.V(3).Infof(logString(fmt.Sprintf("handle microservice instance for agreement %v ended.", agreementId)))

	// delete the agreement from the microservice instance and upgrade the microservice if needed
	if ms_instances, err := persistence.FindMicroserviceInstances(w.db, []persistence.MIFilter{persistence.UnarchivedMIFilter()}); err != nil {
		glog.Errorf(logString(fmt.Sprintf("error retrieving all microservice instances from database, error: %v", err)))
	} else if ms_instances != nil {
		for _, msi := range ms_instances {
			if msi.AssociatedAgreements != nil && len(msi.AssociatedAgreements) > 0 {
				for _, id := range msi.AssociatedAgreements {
					if id == agreementId {
						if msd, err := persistence.FindMicroserviceDefWithKey(w.db, msi.MicroserviceDefId); err != nil {
							glog.Errorf(logString(fmt.Sprintf("Error retrieving microservice definition %v version %v key %v from database, error: %v", msi.SpecRef, msi.Version, msi.MicroserviceDefId, err)))
							// delete the microservice instance if the sharing mode is "multiple"
						} else if msd.Sharable == exchange.MS_SHARING_MODE_MULTIPLE {
							// mark the ms clean up started and remove all the microservice containers if any
							if _, err := persistence.MicroserviceInstanceCleanupStarted(w.db, msi.GetKey()); err != nil {
								glog.Errorf(logString(fmt.Sprintf("Error setting cleanup start time for microservice instance %v. %v", msi.GetKey(), err)))
							} else if has_wl, err := msi.HasWorkload(w.db); err != nil {
								glog.Errorf(logString(fmt.Sprintf("Error checking if the microservice %v has workload. %v", msi.GetKey(), err)))
							} else if has_wl {
								glog.V(5).Infof(logString(fmt.Sprintf("Removing all the containers for %v", msi.GetKey())))
								w.Messages() <- events.NewMicroserviceCancellationMessage(events.CANCEL_MICROSERVICE, msi.GetKey())
							}
							//remove the agreement from the microservice instance
						} else if _, err := persistence.UpdateMSInstanceAssociatedAgreements(w.db, msi.GetKey(), false, agreementId); err != nil {
							glog.Errorf(logString(fmt.Sprintf("error removing agreement id %v from the microservice db: %v", agreementId, err)))
						}

						// handle inactive microservice upgrade, upgrade the microservice if needed
						if !skipUpgrade {
							w.HandleMicroserviceUpgrade(msi.MicroserviceDefId, true)
						}
						break
					}
				}
			}
		}
	}
}
