FROM golang:1.14.4

WORKDIR /app
COPY go.sum go.mod ./
RUN go mod download 
COPY *.go .
RUN go build -o /panop

WORKDIR /
CMD ["/panop"]