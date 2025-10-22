package crawler

import (
	"crypto/tls"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
)

type Result struct {
	Source string
	URL    string
	Where  string
}

func Crawl(url string, headers map[string]string, allowedDomains []string, inside bool, maxDepth int, maxSize int, subsInScope bool, disableRedirects bool, threads int, proxy *url.URL, insecure bool, timeout int, hostname string, showSource bool, showWhere bool, showJson bool, results chan<- string) {
	// Instantiate default collector
	c := colly.NewCollector(
		// default user agent header
		colly.UserAgent("Mozilla/5.0 (X11; Linux x86_64; rv:78.0) Gecko/20100101 Firefox/78.0"),
		// set custom headers
		colly.Headers(headers),
		// limit crawling to the domain of the specified URL
		colly.AllowedDomains(allowedDomains...),
		// set MaxDepth to the specified depth
		colly.MaxDepth(maxDepth),
		// specify Async for threading
		colly.Async(true),
	)

	// set a page size limit
	if maxSize != -1 {
		c.MaxBodySize = maxSize * 1024
	}

	// if -subs is present, use regex to filter out subdomains in scope.
	if subsInScope {
		c.AllowedDomains = nil
		c.URLFilters = []*regexp.Regexp{regexp.MustCompile(".*(\\.|\\/\\/)" + strings.ReplaceAll(hostname, ".", "\\.") + "((#|\\/|\\?).*)?")}
	}

	// If `-dr` flag provided, do not follow HTTP redirects.
	if disableRedirects {
		c.SetRedirectHandler(func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		})
	}
	// Set parallelism
	c.Limit(&colly.LimitRule{DomainGlob: "*", Parallelism: threads})

	// Print every href found, and visit it
	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Attr("href")
		abs_link := e.Request.AbsoluteURL(link)
		if strings.Contains(abs_link, url) || !inside {
			printResult(link, "href", showSource, showWhere, showJson, results, e)
			e.Request.Visit(link)
		}
	})

	// find and print all the JavaScript files
	c.OnHTML("script[src]", func(e *colly.HTMLElement) {
		printResult(e.Attr("src"), "script", showSource, showWhere, showJson, results, e)
	})

	// find and print all the form action URLs
	c.OnHTML("form[action]", func(e *colly.HTMLElement) {
		printResult(e.Attr("action"), "form", showSource, showWhere, showJson, results, e)
	})

	// add the custom headers
	if headers != nil {
		c.OnRequest(func(r *colly.Request) {
			for header, value := range headers {
				r.Headers.Set(header, value)
			}
		})
	}

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
	}

	if proxy != nil {
		// Skip TLS verification for proxy, if -insecure specified
		transport.Proxy = http.ProxyURL(proxy)
	}

	c.WithTransport(transport)

	if timeout == -1 {
		// Start scraping
		c.Visit(url)
		// Wait until threads are finished
		c.Wait()
	} else {
		finished := make(chan int, 1)

		go func() {
			// Start scraping
			c.Visit(url)
			// Wait until threads are finished
			c.Wait()
			finished <- 0
		}()

		select {
		case <-finished: // the crawling finished before the timeout
			close(finished)
		case <-time.After(time.Duration(timeout) * time.Second): // timeout reached
			log.Println("[timeout] " + url)
		}
	}
}

// print result constructs output lines and sends them to the results chan
func printResult(link string, sourceName string, showSource bool, showWhere bool, showJson bool, results chan<- string, e *colly.HTMLElement) {
	result := e.Request.AbsoluteURL(link)
	whereURL := e.Request.URL.String()
	if result != "" {
		if showJson {
			where := ""
			if showWhere {
				where = whereURL
			}
			bytes, _ := json.Marshal(Result{
				Source: sourceName,
				URL:    result,
				Where:  where,
			})
			result = string(bytes)
		} else if showSource {
			result = "[" + sourceName + "] " + result
		}

		if showWhere && !showJson {
			result = "[" + whereURL + "] " + result
		}

		// If timeout occurs before goroutines are finished, recover from panic that may occur when attempting writing to results to closed results channel
		defer func() {
			if err := recover(); err != nil {
				return
			}
		}()
		results <- result
	}
}
