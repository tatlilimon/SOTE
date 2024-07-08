![sote-logo](https://github.com/tatlilimon/SOTE/assets/43828285/4284dfcc-a4e7-4bf6-ae0e-b243a10439a0)

# SOTE
A messaging client with end-to-end encryption, no central server, self-hosted, communicates over the Tor network and can't be censored. 

Unlike other messaging apps, the app doesn't need any centralized server and users simply connect and communicate with each other. This connection takes place entirely on the tor network and is protected by end-to-end GPG asymmetric encryption.
The application will be written entirely in GO and will not have any graphical interface (for now).
The app will ask the user for a separate password each time they create an account and will encrypt the private keys with AES256. Only a username and password will be required to create an account.
To communicate with each other, users need to use the invitation link (hidden service domain ends with .onion ) from the tor network. The link can also be displayed as a QR from the command line.
<hr>

## Dependencies
* Linux/amd64
* Tor >= 0.4.8.11
* Golang >= 1.21.10
<hr>

## How to Run
 *   Install the libraries: `go mod download`
 *   Compile the node: `go build -o sote-node node/main.go`
 *   Compile the client: `go build -o sote-client client/main.go`
 *   Run the node `./sote-node `
 *   Run client in another bash screen `./sote-client start`
If you do not want to install and run directly to your system. You can also run this service on Docker.
<hr>

## Docker 
You can just run this service on Docker. Required commands are listed below.
* Create docker image from source code    `docker build -t sote .`
* Run the image as container   `docker run --name sote-node sote` | if you want to delete container after close use `docker run --rm --name sote-node sote`
* Execute interactively the client with another bash screen  `docker exec -it sote-node ./sote-client start`
<hr> 


## CURRENT PROBLEMS
The problems I am currently experiencing are listed below in order of importance.

- Dockerized sote can succesfully send addContact requests and messages to non dockerized sote. But the if the receiver side is dockerized sote, it throws 403 http forbidden error.

- If receiver accepts sender's addContactRequest, service throws `unknow error general SOCKS server failure` error and shuts down the sender's client.
But after encountering this error both sender and receiver succesfully adds contacts to their db.
<hr>

### TODOS
Todos are listed in order of importance.

- [ ] I need to run a tor service in the background for each account.
It will collission with 9060,9061 port's may it need to configure itselfs dynamcially. 9061, 9062, 9063, 9064...

- [ ] Implement a bash script that shreds every data-dir* folder.

- [ ] Everytime user logins, node is executing `tor -f path/to/torrc`. So node is creating proccess for every successfull login attempt. This is not preventing to communicate. But it may be some problem. I need to handle this. May I check the active tor proccesses that runs with specified torrc file. If this proccess is running, there is no need to create a new tor proccess that runs on user's torrc file in hidden service.

- [ ] Add tor bridges implementation on your app for users who can not acces tor without bridges in living country. NOTE: the progress of fetching bridges on tor is requires to solve a captcha, I don't know how to solve it in CLI.

## Feel Free to Contribute This Project!
You can help me to developing this app by opening a pull request or issue.
> The project is licensed under GPL 3. This means that you can copy this software and use it anywhere you want, but there is only one condition; you must release it under the GPL 3 license. So if you are going to use this code elsewhere, that project must also be open source.
