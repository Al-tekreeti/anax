pipeline {
    agent {
	node{
		label 'ubuntu18.04-docker-8c-8g'
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
		       #go get github.com/tools/godep
		       make
		       #ls -la /usr/local
		'''
	    }
	}
        stage('Build Anax'){
            steps {
                sh 'echo "Building anax binaries"'
		sh '''
			#!/usr/bin/env bash
			echo $HOME
			echo $WORKSPACE
		'''
            }
        }
    }
}

