pipeline {
    agent none
    stages{
	stage('Build and Test'){
	    matrix{
		agent {
			node{
				label 'ubuntu18.04-docker-8c-8g'
			}
    		}
		axes{
	            axis{
			name 'TEST_VARS'
			values "NOLOOP=1", "NOLOOP=1 TEST_PATTERNS=sall"
		    }
		}
		stages {
			stage('Install Dependencies'){
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
				       make
				       make -C test build-remote
				       make -C test clean 
				       #make -C test test TEST_VARS="NOLOOP=1"
				       make -C test test ${TEST_VARS}
				       #go version
				'''
			    }
			}
			stage('Build Anax'){
			    steps {
				sh 'echo "Building anax binaries"'
				sh '''
					#!/usr/bin/env bash
					#go version
					#make
					#make -C test build-remote
					#make -C test clean 
					#make -C test test TEST_VARS="NOLOOP=1"

				'''
			    }
			}
		    }
	    }
	}
    }
}

