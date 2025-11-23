package tracker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goTorr/internal/torrent"

	"github.com/anacrolix/torrent/bencode"
)

type Tracker struct {
	url             string
	trackerID       string
	interval        time.Duration
	minInterval     time.Duration
	lastRequestTime time.Time
	stopTracker     chan bool
}

type trackerResp struct {
	interval    int
	minInterval int
	id          string
	seeders     int
	leechers    int
	peersList   []string
}

func parseBinaryPeers(peersBytes []byte) ([]string, error) {
	if len(peersBytes)%6 != 0 {
		return nil, errors.New("binary Model of peers list must be a multiple of 6")
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
	reqBuf = binary.BigEndian.AppendUint32(reqBuf, 0) // action = 0
	reqBuf = append(reqBuf, tID...)
	conn.Write(reqBuf)

	// Connect Response
	conn.SetReadDeadline(time.Now().Add(25 * time.Second))
	respBuf := make([]byte, 16)
	n, _, err := conn.ReadFromUDP(respBuf)
	conn.SetReadDeadline(time.Time{}) // reset timeout
	if err != nil {
		return nil, err
	}
	if n < 16 {
		return nil, errors.New("incorrent connect response from the tracker")
	}
	respAction := binary.BigEndian.Uint32(respBuf[:4])
	resptID := respBuf[4:8]
	connID = respBuf[8:]
	if respAction != 0 {
		return nil, fmt.Errorf("incorrent action %d sent by tracker, expecting 0", respAction)
	}
	if !bytes.Equal(tID, resptID) {
		return nil, fmt.Errorf("different transactionID used by tracker")
	}
	return connID, nil
}

func sendUDPAnnounce(
	conn *net.UDPConn,
	tID []byte,
	connID []byte,
	torrent *torrent.Torrent,
	peerID []byte,
) (resp trackerResp, err error) {
	announceBuf := make([]byte, 0, 98)
	announceBuf = append(announceBuf, connID...)
	announceBuf = binary.BigEndian.AppendUint32(announceBuf, 1) // action
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

	_, err = conn.Write(announceBuf)
	if err != nil {
		return resp, err
	}

	// Announce Response
	conn.SetReadDeadline(time.Now().Add(25 * time.Second))
	respBuf := make([]byte, 1500)
	n, _, err := conn.ReadFromUDP(respBuf)
	if err != nil {
		return resp, err
	}

	if n < 20 {
		return resp, fmt.Errorf("announe Response too short")
	}

	respAction := binary.BigEndian.Uint32(respBuf[:4])
	resptID := respBuf[4:8]
	if respAction != 1 {
		return resp, fmt.Errorf("incorrent action %d sent by tracker, expecting 0", respAction)
	}
	if !bytes.Equal(tID, resptID) {
		return resp, fmt.Errorf("different transactionID used by tracker")
	}

	interval := binary.BigEndian.Uint32(respBuf[8:12])
	seeders := binary.BigEndian.Uint32(respBuf[12:16])
	leechers := binary.BigEndian.Uint32(respBuf[16:20])
	peersList, err := parseBinaryPeers(respBuf[20:n])
	if err != nil {
		return resp, fmt.Errorf("peer parsing failed, %v", err)
	}
	return trackerResp{
		interval:    int(interval),
		minInterval: int(interval),
		seeders:     int(seeders),
		leechers:    int(leechers),
		peersList:   peersList,
	}, nil
}

func SendUDPTrackerAnnounce(
	announceURL string,
	torrent *torrent.Torrent,
	peerID []byte,
) (resp trackerResp, err error) {
	tID := make([]byte, 4)
	binary.BigEndian.PutUint32(tID, rand.Uint32())

	hostport, _ := strings.CutPrefix(announceURL, "udp://")
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

	failureReason, isIn := respMap["failure reason"]
	if isIn {
		err = errors.New(failureReason.(string))
		return
	}

	trackerid := ""
	_, isIn = respMap["tracker id"]
	if isIn {
		trackerid = respMap["tracker id"].(string)
	}
	interval := respMap["interval"].(int64)
	minInterval := interval
	respMinInterval, isIn := respMap["min interval"]
	if isIn {
		minInterval = respMinInterval.(int64)
	}
	seeders := int64(-1)
	leechers := int64(-1)
	_, isIn = respMap["complete"]
	if isIn {
		seeders = respMap["complete"].(int64)
	}
	_, isIn = respMap["incomplete"]
	if isIn {
		seeders = respMap["incomplete"].(int64)
	}

	var peersList []string
	switch decodedPeers := respMap["peers"].(type) {
	case string:
		peersList, _ = parseBinaryPeers([]byte(decodedPeers))
	case []map[string]any:
		peersList, _ = parseDictionaryPeers(decodedPeers)
	}
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
	announceURL string,
	torrent *torrent.Torrent,
	peerID []byte,
	trackerID string,
) (resp trackerResp, err error) {
	u, err := url.Parse(announceURL)
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

	switch torrent.Downloaded {
	case 0:
		query.Add("event", "started")
	case torrent.TotalSize:
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
	torrent *torrent.Torrent,
	peerID []byte,
) (peer []string, err error) {
	announceURL := tracker.url
	var resp trackerResp
	if strings.HasPrefix(announceURL, "http") {
		resp, err = SendHTTPTrackerAnnounce(announceURL, torrent, peerID, tracker.trackerID)
	} else if strings.HasPrefix(announceURL, "udp") {
		resp, err = SendUDPTrackerAnnounce(announceURL, torrent, peerID)
	} else {
		return nil, fmt.Errorf("unrecognized announce url, %s", announceURL)
	}
	if err != nil {
		return nil, err
	}

	interval, _ := time.ParseDuration(fmt.Sprintf("%ds", resp.interval))
	minInterval, _ := time.ParseDuration(fmt.Sprintf("%ds", resp.minInterval))

	tracker.trackerID = resp.id
	tracker.interval = interval
	tracker.minInterval = minInterval
	tracker.lastRequestTime = time.Now()
	return resp.peersList, nil
}

func (tracker *Tracker) startPeriodicUpdate(
	interval time.Duration,
	torrent *torrent.Torrent,
	peerID []byte,
	pm peerManager,
) (peer []string, err error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
		case <-tracker.stopTracker:
			return
		}
		slog.Info(fmt.Sprintf("Sending Update to %s", tracker.url))
		plist, err := tracker.SendTrackerAnnounce(torrent, peerID)
		if err != nil {
			slog.Debug(fmt.Sprintf("Announce failed on %s, error = %v", tracker.url, err))
			continue
		}
		pm.UpdatePeerList(plist)
	}
}
