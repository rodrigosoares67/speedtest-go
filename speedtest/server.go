package speedtest

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	speedTestServersUrl            = "https://www.speedtest.net/api/js/servers"
	speedTestServersAlternativeUrl = "https://www.speedtest.net/speedtest-servers-static.php"
)

type PayloadType int

const (
	JSONPayload PayloadType = iota
	XMLPayload
)

// Server information
type Server struct {
	URL        string        `xml:"url,attr" json:"url"`
	Lat        string        `xml:"lat,attr" json:"lat"`
	Lon        string        `xml:"lon,attr" json:"lon"`
	Name       string        `xml:"name,attr" json:"name"`
	Country    string        `xml:"country,attr" json:"country"`
	Sponsor    string        `xml:"sponsor,attr" json:"sponsor"`
	ID         string        `xml:"id,attr" json:"id"`
	URL2       string        `xml:"url2,attr" json:"url_2"`
	Host       string        `xml:"host,attr" json:"host"`
	Distance   float64       `json:"distance"`
	Latency    time.Duration `json:"latency"`
	MaxLatency time.Duration `json:"max_latency"`
	MinLatency time.Duration `json:"min_latency"`
	Jitter     time.Duration `json:"jitter"`
	DLSpeed    float64       `json:"dl_speed"`
	ULSpeed    float64       `json:"ul_speed"`

	Context *Speedtest
}

// CustomServer use defaultClient, given a URL string, return a new Server object, with as much
// filled in as we can
func CustomServer(host string) (*Server, error) {
	return defaultClient.CustomServer(host)
}

// CustomServer given a URL string, return a new Server object, with as much
// filled in as we can
func (s *Speedtest) CustomServer(host string) (*Server, error) {
	if !strings.HasSuffix(host, "/upload.php") {
		return nil, errors.New("please use the full URL of the server, ending in '/upload.php'")
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, err
	}
	return &Server{
		ID:      "Custom",
		Lat:     "?",
		Lon:     "?",
		Country: "?",
		URL:     host,
		Name:    u.Host,
		Host:    u.Host,
		Sponsor: "?",
		Context: s,
	}, nil
}

// ServerList list of Server
type ServerList struct {
	Servers []*Server `xml:"servers>server"`
}

// Servers for sorting servers.
type Servers []*Server

// ByDistance for sorting servers.
type ByDistance struct {
	Servers
}

func (servers Servers) Available() *Servers {
	retServer := Servers{}
	for _, server := range servers {
		if server.Latency != PingTimeout {
			retServer = append(retServer, server)
		}
	}
	for i := 0; i < len(retServer)-1; i++ {
		for j := 0; j < len(retServer)-i-1; j++ {
			if retServer[j].Latency > retServer[j+1].Latency {
				retServer[j], retServer[j+1] = retServer[j+1], retServer[j]
			}
		}
	}
	return &retServer
}

// Len finds length of servers. For sorting servers.
func (servers Servers) Len() int {
	return len(servers)
}

// Swap swaps i-th and j-th. For sorting servers.
func (servers Servers) Swap(i, j int) {
	servers[i], servers[j] = servers[j], servers[i]
}

// Less compares the distance. For sorting servers.
func (b ByDistance) Less(i, j int) bool {
	return b.Servers[i].Distance < b.Servers[j].Distance
}

// FetchServers retrieves a list of available servers
func (s *Speedtest) FetchServers() (Servers, error) {
	return s.FetchServerListContext(context.Background())
}

// FetchServers retrieves a list of available servers
func FetchServers() (Servers, error) {
	return defaultClient.FetchServers()
}

// FetchServerListContext retrieves a list of available servers, observing the given context.
func (s *Speedtest) FetchServerListContext(ctx context.Context) (Servers, error) {
	u, err := url.Parse(speedTestServersUrl)
	if err != nil {
		return Servers{}, err
	}
	query := u.Query()
	if len(s.config.Keyword) > 0 {
		query.Set("search", s.config.Keyword)
	}
	if s.config.Location != nil {
		query.Set("lat", strconv.FormatFloat(s.config.Location.Lat, 'f', -1, 64))
		query.Set("lon", strconv.FormatFloat(s.config.Location.Lon, 'f', -1, 64))
	}
	u.RawQuery = query.Encode()
	dbg.Printf("Retrieving servers: %s\n", u.String())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Servers{}, err
	}

	resp, err := s.doer.Do(req)
	if err != nil {
		return Servers{}, err
	}

	payloadType := JSONPayload

	if resp.ContentLength == 0 {
		resp.Body.Close()

		req, err = http.NewRequestWithContext(ctx, http.MethodGet, speedTestServersAlternativeUrl, nil)
		if err != nil {
			return Servers{}, err
		}

		resp, err = s.doer.Do(req)
		if err != nil {
			return Servers{}, err
		}

		payloadType = XMLPayload
	}

	defer resp.Body.Close()

	var servers Servers

	switch payloadType {
	case JSONPayload:
		// Decode xml
		decoder := json.NewDecoder(resp.Body)

		if err = decoder.Decode(&servers); err != nil {
			return servers, err
		}
	case XMLPayload:
		var list ServerList
		// Decode xml
		decoder := xml.NewDecoder(resp.Body)

		if err = decoder.Decode(&list); err != nil {
			return servers, err
		}

		servers = list.Servers
	default:
		return servers, fmt.Errorf("response payload decoding not implemented")
	}

	dbg.Printf("Servers Num: %d\n", len(servers))
	// set doer of server
	for _, server := range servers {
		server.Context = s
	}

	// ping once
	var wg sync.WaitGroup
	pCtx, fc := context.WithTimeout(context.Background(), time.Second*4)
	dbg.Println("Echo each server...")
	for _, server := range servers {
		wg.Add(1)
		go func(gs *Server) {
			var latency []int64
			if s.config.ICMP {
				latency, err = gs.ICMPPing(pCtx, 4*time.Second, 1, time.Millisecond, nil)
			} else {
				latency, err = gs.HTTPPing(pCtx, 1, time.Millisecond, nil)
			}
			if err != nil || len(latency) < 1 {
				gs.Latency = PingTimeout
			} else {
				gs.Latency = time.Duration(latency[0]) * time.Nanosecond
			}
			wg.Done()
		}(server)
	}
	wg.Wait()
	fc()

	// Calculate distance
	// If we don't call FetchUserInfo() before FetchServers(),
	// we don't calculate the distance, instead we use the
	// remote computing distance provided by Ookla as default.
	if s.User != nil {
		for _, server := range servers {
			sLat, _ := strconv.ParseFloat(server.Lat, 64)
			sLon, _ := strconv.ParseFloat(server.Lon, 64)
			uLat, _ := strconv.ParseFloat(s.User.Lat, 64)
			uLon, _ := strconv.ParseFloat(s.User.Lon, 64)
			server.Distance = distance(sLat, sLon, uLat, uLon)
		}
	}

	// Sort by distance
	sort.Sort(ByDistance{servers})

	if len(servers) <= 0 {
		return servers, errors.New("unable to retrieve server list")
	}
	return servers, nil
}

// FetchServerListContext retrieves a list of available servers, observing the given context.
func FetchServerListContext(ctx context.Context) (Servers, error) {
	return defaultClient.FetchServerListContext(ctx)
}

func distance(lat1 float64, lon1 float64, lat2 float64, lon2 float64) float64 {
	radius := 6378.137

	a1 := lat1 * math.Pi / 180.0
	b1 := lon1 * math.Pi / 180.0
	a2 := lat2 * math.Pi / 180.0
	b2 := lon2 * math.Pi / 180.0

	x := math.Sin(a1)*math.Sin(a2) + math.Cos(a1)*math.Cos(a2)*math.Cos(b2-b1)
	return radius * math.Acos(x)
}

// FindServer finds server by serverID
func (servers Servers) FindServer(serverID []int) (Servers, error) {
	retServer := Servers{}

	if len(servers) <= 0 {
		return retServer, errors.New("no servers available")
	}

	for _, sid := range serverID {
		for _, s := range servers {
			id, _ := strconv.Atoi(s.ID)
			if sid == id {
				retServer = append(retServer, s)
			}
		}
	}

	if len(retServer) == 0 {
		// choose the lowest latency server
		var min int64 = math.MaxInt64
		var minServerIndex int
		for index, server := range servers {
			if server.Latency <= 0 {
				continue
			}
			if min > server.Latency.Milliseconds() {
				min = server.Latency.Milliseconds()
				minServerIndex = index
			}
		}
		retServer = append(retServer, servers[minServerIndex])
	}
	return retServer, nil
}

// String representation of ServerList
func (servers ServerList) String() string {
	slr := ""
	for _, server := range servers.Servers {
		slr += server.String()
	}
	return slr
}

// String representation of Servers
func (servers Servers) String() string {
	slr := ""
	for _, server := range servers {
		slr += server.String()
	}
	return slr
}

// String representation of Server
func (s *Server) String() string {
	if s.Sponsor == "?" {
		return fmt.Sprintf("[%4s] %s", s.ID, s.Name)
	}
	return fmt.Sprintf("[%4s] %.2fkm %s (%s) by %s", s.ID, s.Distance, s.Name, s.Country, s.Sponsor)
}

// CheckResultValid checks that results are logical given UL and DL speeds
func (s *Server) CheckResultValid() bool {
	return !(s.DLSpeed*100 < s.ULSpeed) || !(s.DLSpeed > s.ULSpeed*100)
}
