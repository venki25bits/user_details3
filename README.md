The purpose of this template is to increase the speed at which applications are developed and stood up. As well as create a coding standard amongst our teams.

# user-details

user-details serves as a template api

## Usage
To find out how to use this API, please read below instructions:
1) Download the folder and place it under C:\Go\src
2) Run the application using Launch button in Visual Studio Code, the project should run successfully with no errors
3) Open PostMan/Chrome and hit http://localhost:3000/health , the ourput should be returning OK
4) Make changes to datasource.json file to connect with your local databases
5) Run the application again using Launch button in Visual Studio Code, the project should run successfully with no errors
6) Run the application and hit http://localhost:3000/ready , the output should be returning OK . This will make sure that the database connections are successful.

## Testing



## Environment Variables
Environment variables needed to run the application:

- PORT
  - Port to run application. Defaults to: `3000`
- SHUTDOWN_TIMEOUT
  - Graceful timeout period. Defaults to: `25`

## When adding Dependencies
After you add new packages/dependencies to the application, please make sure to run 
these commands in this order at the root of your application project.

- `go get ./...`
- `go mod tidy`
- `go mod vendor`