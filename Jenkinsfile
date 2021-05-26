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
		       rm -rf /usr/local/go && tar -C /usr/local -xzf go${GO_VERSION}.linux-amd64.tar.gz
		       export PATH=$PATH:/usr/local/go/bin
		       #/usr/local/go/bin/go get github.com/tools/godep
		       go version
		       #ls -la /usr/local
		'''
	    }
	}
        stage('Build Anax'){
            steps {
                sh 'echo "Building anax binaries"'
		sh '''
			#!/usr/bin/env bash
		'''
            }
        }
    }
}

