package tracker

import (
	"errors"
	"fmt"
	"goTorr/internal/torrent"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/anacrolix/torrent/bencode"
)

type Tracker struct {
	trackerId       string
	interval        time.Duration
	minInterval     time.Duration
	lastRequestTime time.Time
}

var trackerMap = make(map[string]Tracker)

func SendTrackerAnnounce(
	announce_url string,
	torrent *torrent.Torrent,
	peerID []byte,
) (peer []string, err error) {

	// TODO: Need to handle UDP tracker

	u, err := url.Parse(announce_url)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	query.Add("info_hash", string(torrent.InfoHash[:]))
	query.Add("peer_id", string(peerID))
	query.Add("port", "6869")
	query.Add("uploaded", string(torrent.Uploaded))
	query.Add("downloaded", string(torrent.Downloaded))
	query.Add("left", string(torrent.TotalSize-torrent.Downloaded))
	// query.Add("compact", "1")
	query.Add("no_peer_id", "1")

	if torrent.Downloaded == 0 {
		query.Add("event", "started")
	} else if torrent.Downloaded == torrent.TotalSize {
		query.Add("event", "completed")
	}

	tracker, ok := trackerMap[announce_url]
	if ok {
		if time.Now().After(tracker.lastRequestTime.Add(tracker.interval)) {
			return nil, errors.New("New request sooner than tracker's requested interval")
		}
		if len(tracker.trackerId) > 0 {
			query.Add("trackerid", tracker.trackerId)
		}
	}
	u.RawQuery = query.Encode()

	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/plain")
	client := &http.Client{Timeout: 25 * time.Second}

	resp, err := client.Do(req)

	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP Error (%d) :%w", resp.StatusCode, err)
	}
	resp_byte, _ := io.ReadAll(resp.Body)

	resp_map := make(map[string]interface{})
	err = bencode.Unmarshal(resp_byte, &resp_map)
	if err != nil {
		return nil, err
	}

	failure_reason, isin := resp_map["failure reason"]
	if isin {
		return nil, errors.New(failure_reason.(string))
	}

	trackerid := ""
	_, isin = resp_map["tracker id"]
	if isin {
		trackerid = resp_map["tracker id"].(string)
	}
	interval, _ := time.ParseDuration(fmt.Sprintf("%ds", resp_map["interval"].(int64)))
	resp_minInterval, isin := resp_map["min interval"]
	var minInterval time.Duration
	if isin {
		minInterval, _ = time.ParseDuration(fmt.Sprintf("%ds", resp_minInterval.(int64)))
	} else {
		minInterval = interval
	}

	trackerMap[announce_url] = Tracker{trackerid, interval, minInterval, time.Now()}

	/*peers_bytes := []byte(resp_map["peers"].(string))
	peers_list := make([]string, 0, len(peers_bytes)/6) // Tracker default to 50 peers
	for i := 0; i < len(peers_bytes); i += 6 {
		peers_list = append(
			peers_list,
			fmt.Sprintf("%s:%d", net.IP(peers_bytes[:i+4]), binary.BigEndian.Uint16(peers_bytes[i+4:i+6])),
		)
	}*/

	resp_peers := resp_map["peers"].([]interface{})
	peers_list := make([]string, 0, len(resp_peers))
	for _, peer_map := range resp_peers {
		peer_map := peer_map.(map[string]interface{})
		peers_list = append(
			peers_list,
			fmt.Sprintf("%s:%d", peer_map["ip"].(string), peer_map["port"].(int64)),
		)
	}

	return peers_list, nil

}
