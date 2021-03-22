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
		   	echo ${GOPATH}
		'''
            }
        }
    }
}

