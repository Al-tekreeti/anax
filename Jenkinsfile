pipeline {
    agent any
    stages {
        stage('Build Anax'){
            steps {
                sh 'echo "Building anax binaries"'
                sh 'echo $PATH'
		sh '''
			#!/usr/bin/env bash
		   	go version
			which go
			echo $PATH
			echo $GOPATH
			echo $PWD
		'''
            }
        }
    }
}

