package dm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const (
	roomInitURL   = "https://api.live.bilibili.com/room/v1/Room/room_init?id=%d"
	danmuInfoURL  = "https://api.live.bilibili.com/xlive/web-room/v1/index/getDanmuInfo?id=%d"
	defaultWSSHost = "broadcastlv.chat.bilibili.com"
	defaultWSSPort = 443
)

// roomInfo holds the result of resolving a room ID.
type roomInfo struct {
	RealRoomID int64
}

// danmuInfo holds WebSocket connection details.
type danmuInfo struct {
	Token string
	Host  string
	Port  int
}

// getRoomInfo resolves a (possibly short) room ID to the real room ID.
func getRoomInfo(ctx context.Context, hc *http.Client, roomID int64, cookies string) (*roomInfo, error) {
	url := fmt.Sprintf(roomInitURL, roomID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req, cookies)

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("room_init request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("room_init HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read room_init response: %w", err)
	}

	var result struct {
		Code int `json:"code"`
		Data struct {
			RoomID int64 `json:"room_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse room_init: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("room_init code %d (room %d may not exist)", result.Code, roomID)
	}

	return &roomInfo{RealRoomID: result.Data.RoomID}, nil
}

// getDanmuInfo fetches the WebSocket server host and auth token.
func getDanmuInfo(ctx context.Context, hc *http.Client, realRoomID int64, cookies string) (*danmuInfo, error) {
	url := fmt.Sprintf(danmuInfoURL, realRoomID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setCommonHeaders(req, cookies)

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getDanmuInfo request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getDanmuInfo HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read getDanmuInfo response: %w", err)
	}

	var result struct {
		Code int `json:"code"`
		Data struct {
			Token    string `json:"token"`
			HostList []struct {
				Host    string `json:"host"`
				WSSPort int    `json:"wss_port"`
			} `json:"host_list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parse getDanmuInfo: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("getDanmuInfo code %d", result.Code)
	}

	info := &danmuInfo{
		Token: result.Data.Token,
		Host:  defaultWSSHost,
		Port:  defaultWSSPort,
	}
	if len(result.Data.HostList) > 0 {
		info.Host = result.Data.HostList[0].Host
		info.Port = result.Data.HostList[0].WSSPort
	}

	return info, nil
}

func setCommonHeaders(req *http.Request, cookies string) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://live.bilibili.com/")
	req.Header.Set("Origin", "https://live.bilibili.com")
	if cookies != "" {
		req.Header.Set("Cookie", cookies)
	}
}
