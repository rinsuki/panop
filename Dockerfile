FROM golang:1.15.2-alpine

WORKDIR /app
COPY go.sum go.mod ./
RUN go mod download 
COPY *.go .
RUN go build -o /panop

EXPOSE 53
EXPOSE 53/udp

WORKDIR /
CMD ["/panop"]