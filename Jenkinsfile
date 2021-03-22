pipeline {
    agent any
    environment {
	GOPATH = '/home/mustafa/go'
    }
    stages {
        stage('Build Anax'){
            steps {
                sh 'echo "Building anax binaries"'
		sh '''
			#!/usr/bin/env bash
			ln -s $WORKSPACE $HOME/Projects/github.com/Al-tekreeti/anax
			make
		'''
            }
        }
    }
}

