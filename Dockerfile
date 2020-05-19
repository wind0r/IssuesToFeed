FROM golang:latest as build
WORKDIR /go/src/github.com/wind0r/IssuesToFeed

COPY go.mod .
COPY go.sum .
COPY main.go .

RUN CGO_ENABLED=0 GOOS=linux go build -a .

FROM alpine:latest  
RUN apk add --no-cache ca-certificates
COPY --from=build /go/src/github.com/wind0r/IssuesToFeed/IssuesToFeed ./issuestofeed

ENV RSS_FEED_GITHUB_TOKEN=YOUR_TOKEN
ENV RSS_FEED_DEFAULT_ORG=wind0r
ENV RSS_FEED_DEFAULT_REPO=rss_test
ENV RSS_FEED_DEFAULT_LABEL=team

ENTRYPOINT ["./issuestofeed"]