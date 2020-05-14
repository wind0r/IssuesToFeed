# Issue Comments to RSS

[On Twitter Oliver asked for some tool that generates an RSS feed for events on specific labeled issues.](https://twitter.com/othylmann/status/1260540999291604993)  
So here it is.  
This was done for fun.  
There will be bugs.  
It is not well write.  
There isn't any documentation.  
There isn't a single test.  
Do not tell anybody about this.  
Use on your own risk.

## Usage

[You will need a OAuth Token because of Rate limiting.](https://github.com/settings/tokens)  
You need to set following ENV Variables. I hope those are self-explanatory, otherwise use the source :)

- RSS_FEED_GITHUB_TOKEN=YOUR_TOKEN
- RSS_FEED_DEFAULT_ORG=wind0r
- RSS_FEED_DEFAULT_REPO=rss_test
- RSS_FEED_DEFAULT_LABEL=team

Check the logs for the feed hash and open localhost:8080/{json,rss,atom}/\$HASH to read the feed.
