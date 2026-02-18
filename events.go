package dm

import (
	"encoding/json"
	"time"
)

// Event type constants.
const (
	EventDanmaku     = "danmaku"
	EventGift        = "gift"
	EventSuperChat   = "superchat"
	EventGuardBuy    = "guard"
	EventLive        = "live"
	EventPreparing   = "preparing"
	EventInteract    = "interact"
	EventRaw         = "raw"
	EventHeartbeat   = "heartbeat"
)

// Event is the unified envelope delivered to subscribers.
type Event struct {
	RoomID int64
	Type   string
	Data   interface{}
}

// Danmaku represents a chat message.
type Danmaku struct {
	Sender      string
	UID         int64
	Content     string
	Timestamp   time.Time
	MedalName   string
	MedalLevel  int
	EmoticonURL string
}

// Gift represents a gift event.
type Gift struct {
	User     string
	UID      int64
	GiftName string
	GiftID   int64
	Num      int
	Price    int64 // in gold/silver coins
	CoinType string
	Action   string
}

// SuperChat represents a Super Chat message.
type SuperChat struct {
	User     string
	UID      int64
	Message  string
	Price    int64 // in CNY
	Duration int   // display duration in seconds
}

// GuardBuy represents a captain/admiral/governor purchase.
type GuardBuy struct {
	User       string
	UID        int64
	GuardLevel int // 1=总督, 2=提督, 3=舰长
	Price      int64
	Num        int
}

// LiveEvent represents a room going live or offline.
type LiveEvent struct {
	RoomID int64
	Live   bool
}

// InteractWord represents user interactions (entry, follow, share).
type InteractWord struct {
	User    string
	UID     int64
	MsgType int // 1=entry, 2=follow, 3=share
}

// HeartbeatData carries the popularity value from heartbeat responses.
type HeartbeatData struct {
	Popularity uint32
}

// rawCmd is the top-level JSON structure for command packets.
type rawCmd struct {
	CMD  string          `json:"cmd"`
	Info json.RawMessage `json:"info,omitempty"` // DANMU_MSG uses info array
	Data json.RawMessage `json:"data,omitempty"` // most others use data object
}

// parseCommandPacket turns a raw JSON command body into an Event.
// Returns nil if the command is not recognised (caller can use OnRawEvent).
func parseCommandPacket(roomID int64, body []byte) *Event {
	var cmd rawCmd
	if err := json.Unmarshal(body, &cmd); err != nil {
		return nil
	}

	switch cmd.CMD {
	case "DANMU_MSG":
		return parseDanmaku(roomID, cmd.Info)
	case "SEND_GIFT":
		return parseGift(roomID, cmd.Data)
	case "SUPER_CHAT_MESSAGE":
		return parseSuperChat(roomID, cmd.Data)
	case "GUARD_BUY":
		return parseGuardBuy(roomID, cmd.Data)
	case "LIVE":
		return &Event{RoomID: roomID, Type: EventLive, Data: &LiveEvent{RoomID: roomID, Live: true}}
	case "PREPARING":
		return &Event{RoomID: roomID, Type: EventPreparing, Data: &LiveEvent{RoomID: roomID, Live: false}}
	case "INTERACT_WORD":
		return parseInteractWord(roomID, cmd.Data)
	default:
		return nil // unrecognised — will be dispatched as raw event
	}
}

func parseDanmaku(roomID int64, raw json.RawMessage) *Event {
	// info is a heterogeneous JSON array:
	//  [0]: metadata array, [1]: content string, [2]: user array, [3]: medal array, ...
	var info []json.RawMessage
	if err := json.Unmarshal(raw, &info); err != nil || len(info) < 3 {
		return nil
	}

	d := &Danmaku{}

	// info[1] = message text
	_ = json.Unmarshal(info[1], &d.Content)

	// info[2] = [uid, username, ...]
	var userArr []json.RawMessage
	if err := json.Unmarshal(info[2], &userArr); err == nil && len(userArr) >= 2 {
		_ = json.Unmarshal(userArr[0], &d.UID)
		_ = json.Unmarshal(userArr[1], &d.Sender)
	}

	// info[0][4] = timestamp (seconds)
	var metaArr []json.RawMessage
	if err := json.Unmarshal(info[0], &metaArr); err == nil && len(metaArr) > 4 {
		var ts int64
		if json.Unmarshal(metaArr[4], &ts) == nil && ts > 0 {
			d.Timestamp = time.Unix(ts/1000, (ts%1000)*int64(time.Millisecond))
		}
	}

	// info[3] = medal info [medal_level, medal_name, ...] (may be empty)
	if len(info) > 3 {
		var medalArr []json.RawMessage
		if err := json.Unmarshal(info[3], &medalArr); err == nil && len(medalArr) >= 2 {
			_ = json.Unmarshal(medalArr[0], &d.MedalLevel)
			_ = json.Unmarshal(medalArr[1], &d.MedalName)
		}
	}

	// info[0][13] = emoticon info object (may contain url)
	if len(metaArr) > 13 {
		var emoticonObj map[string]interface{}
		if json.Unmarshal(metaArr[13], &emoticonObj) == nil {
			if u, ok := emoticonObj["url"].(string); ok {
				d.EmoticonURL = u
			}
		}
	}

	return &Event{RoomID: roomID, Type: EventDanmaku, Data: d}
}

func parseGift(roomID int64, raw json.RawMessage) *Event {
	var data struct {
		UID      int64  `json:"uid"`
		Uname    string `json:"uname"`
		GiftName string `json:"giftName"`
		GiftID   int64  `json:"giftId"`
		Num      int    `json:"num"`
		Price    int64  `json:"price"`
		CoinType string `json:"coin_type"`
		Action   string `json:"action"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return &Event{
		RoomID: roomID,
		Type:   EventGift,
		Data: &Gift{
			User:     data.Uname,
			UID:      data.UID,
			GiftName: data.GiftName,
			GiftID:   data.GiftID,
			Num:      data.Num,
			Price:    data.Price,
			CoinType: data.CoinType,
			Action:   data.Action,
		},
	}
}

func parseSuperChat(roomID int64, raw json.RawMessage) *Event {
	var data struct {
		UID      int64 `json:"uid"`
		UserInfo struct {
			Uname string `json:"uname"`
		} `json:"user_info"`
		Message string `json:"message"`
		Price   int64  `json:"price"`
		Time    int    `json:"time"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return &Event{
		RoomID: roomID,
		Type:   EventSuperChat,
		Data: &SuperChat{
			User:     data.UserInfo.Uname,
			UID:      data.UID,
			Message:  data.Message,
			Price:    data.Price,
			Duration: data.Time,
		},
	}
}

func parseGuardBuy(roomID int64, raw json.RawMessage) *Event {
	var data struct {
		UID        int64  `json:"uid"`
		Username   string `json:"username"`
		GuardLevel int    `json:"guard_level"`
		Price      int64  `json:"price"`
		Num        int    `json:"num"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return &Event{
		RoomID: roomID,
		Type:   EventGuardBuy,
		Data: &GuardBuy{
			User:       data.Username,
			UID:        data.UID,
			GuardLevel: data.GuardLevel,
			Price:      data.Price,
			Num:        data.Num,
		},
	}
}

func parseInteractWord(roomID int64, raw json.RawMessage) *Event {
	var data struct {
		UID     int64  `json:"uid"`
		Uname   string `json:"uname"`
		MsgType int    `json:"msg_type"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}
	return &Event{
		RoomID: roomID,
		Type:   EventInteract,
		Data: &InteractWord{
			User:    data.Uname,
			UID:     data.UID,
			MsgType: data.MsgType,
		},
	}
}
