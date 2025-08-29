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

type trackerResp struct {
	interval    int
	minInterval int
	id          string
	seeders     int
	leechers    int
	peersList   []string
}

var trackerMap = make(map[string]Tracker)

func parseBinaryPeers(peersBytes []byte) ([]string, error) {
	if len(peersBytes)%6 != 0 {
		return nil, errors.New("Binary Model of peers list must be a multiple of 6")
	}
	peersList := make([]string, 0, len(peersBytes)/6) // Tracker default to 50 peers
	for i := 0; i < len(peersBytes); i += 6 {
		peersList = append(
			peersList,
			fmt.Sprintf("%s:%d", net.IP(peersBytes[i:i+4]), binary.BigEndian.Uint16(peersBytes[i+4:i+6])),
		)
	}
	return peersList, nil
}

func parseDictionaryPeers(peers []map[string]any) ([]string, error) {
	peersList := make([]string, 0, len(peers))
	for _, peerMap := range peers {
		peersList = append(
			peersList,
			fmt.Sprintf("%s:%d", peerMap["ip"].(string), peerMap["port"].(int64)),
		)
	}
	return peersList, nil
}

func sendUDPConnect(conn *net.UDPConn, tID []byte) (connID []byte, err error) {
	// Connect Request
	reqBuf := make([]byte, 0, 16)
	reqBuf = binary.BigEndian.AppendUint64(reqBuf, uint64(0x41727101980))
	reqBuf = binary.BigEndian.AppendUint32(reqBuf, 0) //action = 0
	reqBuf = append(reqBuf, tID...)
	conn.Write(reqBuf)

	// Connect Response
	conn.SetReadDeadline(time.Now().Add(25 * time.Second))
	respBuf := make([]byte, 16)
	n, _, err := conn.ReadFromUDP(respBuf)
	conn.SetReadDeadline(time.Time{}) //reset timeout
	if err != nil {
		return nil, err
	}
	if n < 16 {
		return nil, errors.New("Incorrent connect response from the tracker")
	}
	respAction := binary.BigEndian.Uint32(respBuf[:4])
	resptID := respBuf[4:8]
	connID = respBuf[8:]
	if respAction != 0 {
		return nil, fmt.Errorf("Incorrent action %d sent by tracker, expecting 0", respAction)
	}
	if !bytes.Equal(tID, resptID) {
		return nil, fmt.Errorf("Different transactionID used by tracker")
	}
	return connID, nil

}

func sendUDPAnnounce(
	conn *net.UDPConn,
	tID []byte,
	connID []byte,
	torrent *torrent.Torrent,
	peerID []byte) (resp trackerResp, err error) {

	announceBuf := make([]byte, 0, 98)
	announceBuf = append(announceBuf, connID...)
	announceBuf = binary.BigEndian.AppendUint32(announceBuf, 1) //action
	announceBuf = append(announceBuf, tID...)
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

	n, err := conn.Write(announceBuf)
	if err != nil {
		return resp, err
	}

	// Announce Response
	conn.SetReadDeadline(time.Now().Add(25 * time.Second))
	respBuf := make([]byte, 1500)
	n, _, err = conn.ReadFromUDP(respBuf)
	if err != nil {
		return resp, err
	}

	if n < 20 {
		return resp, fmt.Errorf("Announe Response too short")
	}

	respAction := binary.BigEndian.Uint32(respBuf[:4])
	resptID := respBuf[4:8]
	if respAction != 1 {
		return resp, fmt.Errorf("Incorrent action %d sent by tracker, expecting 0", respAction)
	}
	if !bytes.Equal(tID, resptID) {
		return resp, fmt.Errorf("Different transactionID used by tracker")
	}

	interval := binary.BigEndian.Uint32(respBuf[8:12])
	seeders := binary.BigEndian.Uint32(respBuf[12:16])
	leechers := binary.BigEndian.Uint32(respBuf[16:20])
	peersList, err := parseBinaryPeers(respBuf[20:n])
	return trackerResp{
		interval:    int(interval),
		minInterval: int(interval),
		seeders:     int(seeders),
		leechers:    int(leechers),
		peersList:   peersList,
	}, nil

}

func SendUDPTrackerAnnounce(
	announce_url string,
	torrent *torrent.Torrent,
	peerID []byte,
) (resp trackerResp, err error) {

	tID := make([]byte, 4)
	binary.BigEndian.PutUint32(tID, rand.Uint32())

	hostport, _ := strings.CutPrefix(announce_url, "udp://")
	hostport, _ = strings.CutSuffix(hostport, "/announce")
	raddr, err := net.ResolveUDPAddr("udp", hostport)
	if err != nil {
		return
	}

	conn, err := net.DialUDP("udp", nil, raddr)
	if err != nil {
		return
	}
	defer conn.Close()

	connID, err := sendUDPConnect(conn, tID)
	if err != nil {
		return
	}
	return sendUDPAnnounce(conn, tID, connID, torrent, peerID)
}

func parseHTTPAnnounceResp(httpResp []byte) (parsedResp trackerResp, err error) {

	respMap := make(map[string]any)
	err = bencode.Unmarshal(httpResp, &respMap)
	if err != nil {
		return
	}

	failure_reason, isin := respMap["failure reason"]
	if isin {
		err = errors.New(failure_reason.(string))
		return
	}

	trackerid := ""
	_, isin = respMap["tracker id"]
	if isin {
		trackerid = respMap["tracker id"].(string)
	}
	interval := respMap["interval"].(int64)
	minInterval := interval
	respMinInterval, isin := respMap["min interval"]
	if isin {
		minInterval = respMinInterval.(int64)
	}
	seeders := int64(-1)
	leechers := int64(-1)
	_, isin = respMap["complete"]
	if isin {
		seeders = respMap["complete"].(int64)
	}
	_, isin = respMap["incomplete"]
	if isin {
		seeders = respMap["incomplete"].(int64)
	}

	decodedPeers := respMap["peers"].([]any)
	peers := make([]map[string]any, len(decodedPeers))
	for i := range decodedPeers {
		peers[i] = decodedPeers[i].(map[string]any)
	}
	peersList, _ := parseDictionaryPeers(peers)
	return trackerResp{
		int(interval),
		int(minInterval),
		trackerid,
		int(seeders),
		int(leechers),
		peersList,
	}, nil

}
func SendHTTPTrackerAnnounce(
	announce_url string,
	torrent *torrent.Torrent,
	peerID []byte,
	trackerID string,
) (resp trackerResp, err error) {

	u, err := url.Parse(announce_url)
	if err != nil {
		return
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

	if len(trackerID) > 0 {
		query.Add("trackerid", trackerID)
	}

	u.RawQuery = query.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/plain")
	client := &http.Client{Timeout: 25 * time.Second}

	httpResp, err := client.Do(req)
	if err != nil {
		return
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		err = fmt.Errorf("HTTP Error (%d) :%w", httpResp.StatusCode, err)
		return
	}

	respBytes, _ := io.ReadAll(httpResp.Body)

	return parseHTTPAnnounceResp(respBytes)
}

func (tracker *Tracker) SendTrackerAnnounce(
	announce_url string,
	torrent *torrent.Torrent,
	peerID []byte,
) (peer []string, err error) {
	//if isin {
	//	if time.Now().After(tracker.lastRequestTime.Add(tracker.interval)) {
	//		return nil, errors.New("New request sooner than tracker's requested interval")
	//	}
	//}
	var resp trackerResp
	if strings.HasPrefix(announce_url, "http") {
		resp, err = SendHTTPTrackerAnnounce(announce_url, torrent, peerID, tracker.trackerId)
	} else if strings.HasPrefix(announce_url, "udp") {
		resp, err = SendUDPTrackerAnnounce(announce_url, torrent, peerID)
	} else {
		return nil, fmt.Errorf("Unrecognized announce url, %s", announce_url)
	}
	if err != nil {
		return nil, err
	}

	interval, _ := time.ParseDuration(fmt.Sprintf("%ds", resp.interval))
	minInterval, _ := time.ParseDuration(fmt.Sprintf("%ds", resp.minInterval))

	tracker.trackerId = resp.id
	tracker.interval = interval
	tracker.minInterval = minInterval
	tracker.lastRequestTime = time.Now()
	return resp.peersList, nil

}
