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
			sudo ln -s $WORKSPACE $GOPATH/Projects/github.com/Al-tekreeti/anax
			make
		'''
            }
        }
    }
}

