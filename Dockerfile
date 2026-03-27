FROM golang:1.25 as build

ARG GIN_MODE=release
ARG BUILD_VERSION=unknown
ENV GIN_MODE=$GIN_MODE
ENV BUILD_VERSION=$BUILD_VERSION
# create a working directory inside the image
WORKDIR /app

# copy Go modules and dependencies to image
COPY go.mod ./
COPY go.sum ./

# download Go modules and dependencies
RUN go mod download

# copy directory files i.e all files ending with .go
COPY ./cmd/main/main.go ./
COPY ./internal ./internal

# compile application with version
RUN go build -ldflags "-X main.buildVersion=${BUILD_VERSION}" -o /app/main ./main.go

FROM ubuntu:latest

WORKDIR /app

COPY --from=build /app/main /app/main
COPY ./templates /app/templates
COPY ./static /app/static

RUN chmod +x /app/main


# command to be used to execute when the image is used to start a container
CMD [ "/app/main" ]