FROM golang:1.21.10

RUN apt-get update && apt-get install -y tor

WORKDIR /app

COPY . .
RUN go mod download

RUN go build -o sote-client ./client/main.go
RUN go build -o sote-node ./node/main.go

EXPOSE 18080
EXPOSE 9050
EXPOSE 9051
EXPOSE 9060
EXPOSE 9061

ENTRYPOINT [ "./sote-node" ] 