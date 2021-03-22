pipeline {
    agent any
    stages {
        stage('Build Anax'){
            steps {
                sh 'echo "Building anax binaries"'
		sh '''
			#!/usr/bin/env bash
		   	go version
			which go
			echo $PATH
			echo $GOPATH
			echo $PWD
			echo $HOME
			ls $PWD -la
			ls $HOME -la
		'''
            }
        }
    }
}

