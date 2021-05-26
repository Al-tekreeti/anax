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
		       ls -la
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

