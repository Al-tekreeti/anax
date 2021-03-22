pipeline {
    agent any
    stages {
        stage('Build Anax'){
            steps {
                sh 'echo "Building anax binaries"'
                sh 'echo $PATH'
		sh '''
		   	go version
			which go
			echo $PATH
		'''
            }
        }
    }
}

