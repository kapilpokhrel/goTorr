package tracker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"goTorr/internal/torrent"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

func SendUDPTrackerAnnounce(
	announce_url string,
	torrent *torrent.Torrent,
	peerID []byte,
) (peer []string, err error) {

	tracker, ok := trackerMap[announce_url]
	if ok {
		if time.Now().After(tracker.lastRequestTime.Add(tracker.interval)) {
			return nil, errors.New("New request sooner than tracker's requested interval")
		}
	}

	// Connect Request
	connectBuf := make([]byte, 0, 16)
	connectBuf = binary.BigEndian.AppendUint64(connectBuf, uint64(0x41727101980))
	connectBuf = binary.BigEndian.AppendUint32(connectBuf, 0) //action = 0

	transactionID := make([]byte, 4)
	binary.BigEndian.PutUint32(transactionID, rand.Uint32())
	connectBuf = append(connectBuf, transactionID...)

	hostport, _ := strings.CutPrefix(announce_url, "udp://")
	hostport, _ = strings.CutSuffix(hostport, "/announce")

	raddr, err := net.ResolveUDPAddr("udp", hostport)
	if err != nil {
		panic(err)
	}

	conn, err := net.DialUDP("udp", nil, raddr)

	if err != nil {
		return nil, err
	}
	defer conn.Close()

	fmt.Printf("Sending %v\n", connectBuf)
	conn.Write(connectBuf)

	// Connect Response
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	readBuf := make([]byte, 16)
	n, err := io.ReadFull(conn, readBuf)
	conn.SetReadDeadline(time.Time{}) //reset timeout
	if err != nil {
		return nil, err
	}
	if n < 16 {
		return nil, errors.New("Incorrent connect response from the tracker")
	}
	connectRespAction := binary.BigEndian.Uint32(readBuf[:4])
	respTransactionID := readBuf[4:8]
	connectionID := readBuf[8:]
	if connectRespAction != 0 {
		return nil, fmt.Errorf("Incorrent action %d sent by tracker, expecting 0", connectRespAction)
	}
	if !bytes.Equal(transactionID, respTransactionID) {
		return nil, fmt.Errorf("Different transactionID used by tracker")
	}

	fmt.Printf("Got Connection ID, %v\n", connectionID)
	fmt.Printf("InfoHash, %v\n", torrent.InfoHash)
	fmt.Printf("PeerID, %v\n", peerID)
	// Announcing
	announceBuf := make([]byte, 0, 98)
	announceBuf = append(announceBuf, connectionID...)
	announceBuf = binary.BigEndian.AppendUint32(announceBuf, 1) //action
	announceBuf = append(announceBuf, transactionID...)
	announceBuf = append(announceBuf, torrent.InfoHash[:]...)
	announceBuf = append(announceBuf, peerID...)
	announceBuf = binary.BigEndian.AppendUint64(announceBuf, uint64(torrent.Downloaded))
	announceBuf = binary.BigEndian.AppendUint64(announceBuf, uint64(torrent.TotalSize)-uint64(torrent.Downloaded))
	announceBuf = binary.BigEndian.AppendUint64(announceBuf, uint64(torrent.Uploaded))
	announceBuf = binary.BigEndian.AppendUint32(announceBuf, 0)             // Event; TODO
	announceBuf = binary.BigEndian.AppendUint32(announceBuf, 0)             // IP
	announceBuf = binary.BigEndian.AppendUint32(announceBuf, rand.Uint32()) // KEY; IDK what is it
	announceBuf = binary.BigEndian.AppendUint32(announceBuf, 50)            // numwant
	announceBuf = binary.BigEndian.AppendUint16(announceBuf, 6869)          // Port

	fmt.Printf("Announcing, %v, Length: %d\n", announceBuf, len(announceBuf))
	n, err = conn.Write(announceBuf)
	if err != nil {
		return nil, err
	}

	// Announce Response
	conn.SetReadDeadline(time.Now().Add(25 * time.Second))
	announceRespBuf := make([]byte, 1500)
	n, _, err = conn.ReadFromUDP(announceRespBuf)
	if err != nil {
		return nil, err
	}

	if n < 20 {
		return nil, fmt.Errorf("Announe Response too short")
	}
	interval, _ := time.ParseDuration(
		fmt.Sprintf("%ds", binary.BigEndian.Uint32(announceRespBuf[8:12])),
	)
	minInternal := interval
	trackerMap[announce_url] = Tracker{"", interval, minInternal, time.Now()}

	peers_list := make([]string, 0)
	if len(announceRespBuf) > 20 {
		peers_bytes := announceRespBuf[20:n]
		peers_list = make([]string, 0, len(peers_bytes)/6) // Tracker default to 50 peers
		for i := 0; i < len(peers_bytes); i += 6 {
			peers_list = append(
				peers_list,
				fmt.Sprintf("%s:%d", net.IP(peers_bytes[i:i+4]), binary.BigEndian.Uint16(peers_bytes[i+4:i+6])),
			)
		}
	}
	fmt.Printf("Got %d peers from tracker %s\n", len(peers_list), announce_url)
	return peers_list, nil

}

func SendHTTPTrackerAnnounce(
	announce_url string,
	torrent *torrent.Torrent,
	peerID []byte,
) (peer []string, err error) {
	u, err := url.Parse(announce_url)
	if err != nil {
		return nil, err
	}
	query := u.Query()
	query.Add("info_hash", string(torrent.InfoHash[:]))
	query.Add("peer_id", string(peerID))
	query.Add("port", "6869")
	query.Add("uploaded", strconv.FormatUint(torrent.Uploaded, 10))
	query.Add("uploaded", strconv.FormatUint(torrent.Downloaded, 10))
	query.Add("uploaded", strconv.FormatUint(torrent.TotalSize-torrent.Downloaded, 10))
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

func SendTrackerAnnounce(
	announce_url string,
	torrent *torrent.Torrent,
	peerID []byte,
) (peer []string, err error) {
	if strings.HasPrefix(announce_url, "http") {
		return SendHTTPTrackerAnnounce(announce_url, torrent, peerID)
	} else if strings.HasPrefix(announce_url, "udp") {
		return SendUDPTrackerAnnounce(announce_url, torrent, peerID)
	}
	return nil, fmt.Errorf("Unrecognized announce url, %s", announce_url)
}
