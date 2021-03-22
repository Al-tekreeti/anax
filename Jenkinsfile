pipeline {
    agent any
    environment {
	GOPATH = '/home/mustafa'
    }
    stages {
        stage('Build Anax'){
            steps {
                sh 'echo "Building anax binaries"'
		sh '''
			#!/usr/bin/env bash
			mkdir -p $HOME/go/src/github.com/Al-tekreeti/anax
			export GOPATH=$HOME/go
			ln -s $WORKSPACE $GOPATH/src/github.com/Al-tekreeti/anax
			make
		'''
            }
        }
    }
}

