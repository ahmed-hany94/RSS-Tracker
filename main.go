package main

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DATABASE_FILE = "sites.json"
	HTTP_TIMEOUT  = 30 * time.Second
	MAX_WORKERS   = 10
)

type FeedType int

const (
	FeedTypeUnknown FeedType = iota
	FeedTypeAtom
	FeedTypeRSS
)

type AtomFeed struct {
	Entries []AtomEntry `xml:"entry"`
}

type AtomEntry struct {
	Title string     `xml:"title"`
	Links []AtomLink `xml:"link"`
}

type AtomLink struct {
	Href string `xml:"href,attr"`
}

type RSSFeed struct {
	Channel RSSChannel `xml:"channel"`
}

type RSSChannel struct {
	Items []RSSItem `xml:"item"`
}

type RSSItem struct {
	Title string `xml:"title"`
	Link  string `xml:"link"`
	Guid  string `xml:"guid"`
}

type Site struct {
	RSSUrl      string `json:"rss_url"`
	LatestEntry string `json:"latest_entry"`
}

type SiteData map[string]Site

type FeedResult struct {
	Title      string
	LatestLink string
	FeedType   FeedType
}

func detectFeedType(body []byte) FeedType {
	content := string(body)

	if strings.Contains(content, "<feed") && strings.Contains(content, "http://www.w3.org/2005/Atom") {
		return FeedTypeAtom
	}

	if strings.Contains(content, "<rss") || strings.Contains(content, "<rdf:RDF") {
		return FeedTypeRSS
	}

	var atom struct {
		XMLName xml.Name `xml:"feed"`
	}
	if err := xml.Unmarshal(body, &atom); err == nil && atom.XMLName.Local == "feed" {
		return FeedTypeAtom
	}

	var rss struct {
		XMLName xml.Name `xml:"rss"`
	}
	if err := xml.Unmarshal(body, &rss); err == nil && rss.XMLName.Local == "rss" {
		return FeedTypeRSS
	}

	return FeedTypeUnknown
}

func parseFeed(body []byte) (*FeedResult, error) {
	feedType := detectFeedType(body)

	switch feedType {
	case FeedTypeAtom:
		return parseAtomFeed(body)
	case FeedTypeRSS:
		return parseRSSFeed(body)
	default:
		return nil, fmt.Errorf("unsupported feed format")
	}
}

func parseAtomFeed(body []byte) (*FeedResult, error) {
	var atom AtomFeed
	if err := xml.Unmarshal(body, &atom); err != nil {
		return nil, fmt.Errorf("parsing Atom feed: %w", err)
	}

	if len(atom.Entries) == 0 {
		return &FeedResult{FeedType: FeedTypeAtom}, nil
	}

	latestEntry := atom.Entries[0]
	latestLink := ""
	if len(latestEntry.Links) > 0 {
		latestLink = strings.TrimSpace(latestEntry.Links[0].Href)
	}

	return &FeedResult{
		Title:      strings.TrimSpace(latestEntry.Title),
		LatestLink: latestLink,
		FeedType:   FeedTypeAtom,
	}, nil
}

func parseRSSFeed(body []byte) (*FeedResult, error) {
	var rss RSSFeed
	if err := xml.Unmarshal(body, &rss); err != nil {
		return nil, fmt.Errorf("parsing RSS feed: %w", err)
	}

	if len(rss.Channel.Items) == 0 {
		return &FeedResult{FeedType: FeedTypeRSS}, nil
	}

	latestItem := rss.Channel.Items[0]
	latestLink := strings.TrimSpace(latestItem.Link)
	if latestLink == "" {
		latestLink = strings.TrimSpace(latestItem.Guid)
	}

	return &FeedResult{
		Title:      strings.TrimSpace(latestItem.Title),
		LatestLink: latestLink,
		FeedType:   FeedTypeRSS,
	}, nil
}

func feedTypeString(feedType FeedType) string {
	switch feedType {
	case FeedTypeAtom:
		return "Atom"
	case FeedTypeRSS:
		return "RSS"
	default:
		return "Unknown"
	}
}

func readSites() (SiteData, error) {
	data, err := os.ReadFile(DATABASE_FILE)
	if err != nil {
		if os.IsNotExist(err) {
			return make(SiteData), nil
		}
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	if len(data) == 0 {
		return make(SiteData), nil
	}

	var sites SiteData
	if err := json.Unmarshal(data, &sites); err != nil {
		return nil, fmt.Errorf("error parsing JSON: %w", err)
	}

	return sites, nil
}

func saveSites(sites SiteData) error {
	data, err := json.MarshalIndent(sites, "", "  ")
	if err != nil {
		return fmt.Errorf("error marshaling JSON: %w", err)
	}

	return os.WriteFile(DATABASE_FILE, data, 0644)
}

func getSiteInput(sites SiteData, reader *bufio.Reader) (string, string, error) {
	for {
		fmt.Print("Enter Site Name: ")
		siteName, err := reader.ReadString('\n')
		if err != nil {
			return "", "", fmt.Errorf("error reading site name: %w", err)
		}
		siteName = strings.TrimSpace(siteName)

		if siteName == "" {
			fmt.Println("Site name cannot be empty")
			continue
		}

		if _, exists := sites[siteName]; exists {
			fmt.Printf("Site '%s' already exists!\n", siteName)
			continue
		}

		fmt.Print("Enter Site RSS URL: ")
		siteRSSURL, err := reader.ReadString('\n')
		if err != nil {
			return "", "", fmt.Errorf("error reading RSS URL: %w", err)
		}
		siteRSSURL = strings.TrimSpace(siteRSSURL)

		if siteRSSURL == "" {
			fmt.Println("RSS URL cannot be empty")
			continue
		}

		return siteName, siteRSSURL, nil
	}
}

func addSiteMode(sites SiteData) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		siteName, siteRSSURL, err := getSiteInput(sites, reader)
		if err != nil {
			return err
		}

		fmt.Printf("Testing feed... ")
		resp, err := http.Get(siteRSSURL)
		if err != nil {
			fmt.Printf("FAILED: %v\n", err)
			fmt.Print("Do you want to save anyway? (y/n): ")
			confirm, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(confirm)) != "y" {
				fmt.Println("Site not saved")
				continue
			}
		} else {
			defer resp.Body.Close()
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				fmt.Printf("FAILED: %v\n", err)
			} else {
				feedType := detectFeedType(body)
				fmt.Printf("OK (%s feed detected)\n", feedTypeString(feedType))
			}
		}

		sites[siteName] = Site{
			RSSUrl:      siteRSSURL,
			LatestEntry: "",
		}

		if err := saveSites(sites); err != nil {
			return fmt.Errorf("saving site: %w", err)
		}

		fmt.Printf("✓ Successfully added '%s'\n", siteName)

		fmt.Print("\nAdd another site? (y/n): ")
		more, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(more)) != "y" {
			break
		}
		fmt.Println()
	}

	return nil
}

func checkFeeds(sites SiteData) error {
	hasUpdates := false

	for name, site := range sites {
		fmt.Printf("Checking %s... ", name)

		resp, err := http.Get(site.RSSUrl)
		if err != nil {
			fmt.Printf("URL fetch error: %v\n", err)
			continue
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Printf("Error reading response: %v\n", err)
			continue
		}

		feedResult, err := parseFeed(body)
		if err != nil {
			fmt.Printf("Parse error: %v\n", err)
			continue
		}

		if feedResult.LatestLink == "" {
			fmt.Printf("no entries found (%s)\n", feedTypeString(feedResult.FeedType))
			continue
		}

		savedLink := strings.TrimSpace(site.LatestEntry)

		switch {
		case savedLink == "":
			fmt.Printf("first time checking (%s)\n", feedTypeString(feedResult.FeedType))
			site.LatestEntry = feedResult.LatestLink
			sites[name] = site
			hasUpdates = true

		case feedResult.LatestLink != savedLink:
			title := feedResult.Title
			if title == "" {
				title = "Untitled"
			}
			fmt.Printf("NEW ENTRY: %s - %s (%s)\n", title, feedResult.LatestLink, feedTypeString(feedResult.FeedType))
			site.LatestEntry = feedResult.LatestLink
			sites[name] = site
			hasUpdates = true

		default:
			fmt.Printf("no new entries (%s)\n", feedTypeString(feedResult.FeedType))
		}
	}

	if hasUpdates {
		if err := saveSites(sites); err != nil {
			return fmt.Errorf("saving updates: %w", err)
		}
		fmt.Println("✓ Site database updated")
	}

	return nil
}

func main() {
	addPtr := flag.Bool("a", false, "Add new site mode.")
	flag.Parse()

	sites, err := readSites()
	if err != nil {
		fmt.Printf("Error reading sites: %v\n", err)
		os.Exit(1)
	}

	if *addPtr {
		if err := addSiteMode(sites); err != nil {
			fmt.Printf("Error in add mode: %v\n", err)
			os.Exit(1)
		}
	} else {
		if len(sites) == 0 {
			fmt.Println("No sites configured. Use -a to add sites.")
			return
		}

		if err := checkFeeds(sites); err != nil {
			fmt.Printf("Error checking feeds: %v\n", err)
			os.Exit(1)
		}
	}
}
