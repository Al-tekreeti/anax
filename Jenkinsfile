pipeline {
    agent any
    stages {
        stage('Build Anax'){
            steps {
                sh 'echo "Building anax binaries"'
		sh '''
			#!/usr/bin/env bash
			mkdir -p $HOME/go/src/github.com/Al-tekreeti/anax
			export GOPATH=$HOME/go
			export PATH=$PATH:/usr/local/go/bin
			#ln -fs $WORKSPACE $GOPATH/src/github.com/Al-tekreeti/anax
			make 
		'''
            }
        }
    }
}

