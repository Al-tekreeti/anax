
pipeline {
   	agent { node { label 'ubuntu18.04-docker-8c-8g' } }
    	stages {
		stage('Install dependencies'){
	    		steps{
				sh 'echo "Installing dependencies"'
				sh '''
		       		#!/usr/bin/env bash
		       		export GO_VERSION=1.14.1
		       		wget https://storage.googleapis.com/golang/go${GO_VERSION}.linux-amd64.tar.gz
		       		sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
		       		export PATH=$PATH:/usr/local/go/bin
				mkdir -p $HOME/go/src/github.com/Al-tekreeti/anax
		       		export GOPATH=$HOME/go
		       		ln -fs $WORKSPACE $GOPATH/src/github.com/Al-tekreeti/anax
				'''
	    		}
		}
        	stage('Build anax'){
			matrix {
				axes {
					axis {
						name "tests"
						values "NOLOOP=1", "NOLOOP=1 TEST_PATTERNS=sloc"
					}
				}
				stages {
					stage('Conduct e2e-dev-test') {
						steps {
							sh 'echo "Building anax binaries"'
							sh '''
							#!/usr/bin/env bash
							export GOPATH=$HOME/go
							export PATH=$PATH:/usr/local/go/bin
							make
							make -C test build-remote
							make -C test clean 
							make -C test test TEST_VARS=${tests}
							'''
            					}
					}
				}
			}
        	}
    	}
}
