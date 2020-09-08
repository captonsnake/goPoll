################
## Build stage
################
FROM golang:1.11-alpine3.8 AS build
RUN apk --no-cache add git gcc musl-dev

# Fetch Go dependencies, if necessary
WORKDIR /src
COPY go.mod .
COPY go.sum .
RUN go mod download

# Build our Go app
COPY . .
RUN go build -o ../bin/go-app


################
## Final stage
################
FROM alpine:3.8

WORKDIR /app
COPY --from=build /bin/go-app .

EXPOSE 4000
USER guest
ENTRYPOINT ./go-app
