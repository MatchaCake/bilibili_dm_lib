package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"

	dm "github.com/MatchaCake/bilibili_dm_lib"
)

func main() {
	roomID := flag.Int64("room", 510, "Bilibili live room ID")
	sessdata := flag.String("sessdata", "", "SESSDATA cookie (optional)")
	biliJCT := flag.String("bili-jct", "", "bili_jct cookie (optional)")
	flag.Parse()

	slog.Info("starting", "room", *roomID)

	opts := []dm.Option{
		dm.WithRoomID(*roomID),
	}
	if *sessdata != "" {
		opts = append(opts, dm.WithCookie(*sessdata, *biliJCT))
	}

	client := dm.NewClient(opts...)

	client.OnDanmaku(func(d *dm.Danmaku) {
		medal := ""
		if d.MedalName != "" {
			medal = fmt.Sprintf("[%s %d] ", d.MedalName, d.MedalLevel)
		}
		fmt.Printf("[弹幕] %s%s: %s\n", medal, d.Sender, d.Content)
	})

	client.OnGift(func(g *dm.Gift) {
		fmt.Printf("[礼物] %s %s %s x%d\n", g.User, g.Action, g.GiftName, g.Num)
	})

	client.OnSuperChat(func(sc *dm.SuperChat) {
		fmt.Printf("[SC ¥%d] %s: %s\n", sc.Price, sc.User, sc.Message)
	})

	client.OnGuardBuy(func(gb *dm.GuardBuy) {
		levels := map[int]string{1: "总督", 2: "提督", 3: "舰长"}
		name := levels[gb.GuardLevel]
		fmt.Printf("[上舰] %s 开通了 %s\n", gb.User, name)
	})

	client.OnLive(func(le *dm.LiveEvent) {
		fmt.Printf("[开播] 房间 %d 开始直播\n", le.RoomID)
	})

	client.OnPreparing(func(le *dm.LiveEvent) {
		fmt.Printf("[下播] 房间 %d 停止直播\n", le.RoomID)
	})

	client.OnInteractWord(func(iw *dm.InteractWord) {
		actions := map[int]string{1: "进入", 2: "关注", 3: "分享"}
		act := actions[iw.MsgType]
		if act == "" {
			act = fmt.Sprintf("互动(%d)", iw.MsgType)
		}
		fmt.Printf("[互动] %s %s了直播间\n", iw.User, act)
	})

	client.OnHeartbeat(func(hb *dm.HeartbeatData) {
		slog.Debug("heartbeat", "popularity", hb.Popularity)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// --- Send danmaku example (uncomment to use) ---
	// Requires valid SESSDATA and bili_jct cookies.
	//
	// go func() {
	// 	time.Sleep(3 * time.Second) // wait for connection
	// 	if err := client.SendDanmaku(ctx, *roomID, "Hello from bilibili_dm_lib!"); err != nil {
	// 		slog.Error("send danmaku failed", "error", err)
	// 	}
	// }()
	//
	// Standalone sender (without subscribing):
	//
	// sender := dm.NewSender(
	// 	dm.WithSenderCookie("your_SESSDATA", "your_bili_jct"),
	// 	dm.WithMaxLength(30), // UL20+ users
	// )
	// if err := sender.Send(ctx, 510, "Hello!"); err != nil {
	// 	slog.Error("send failed", "error", err)
	// }

	if err := client.Start(ctx); err != nil && ctx.Err() == nil {
		slog.Error("client stopped with error", "error", err)
		os.Exit(1)
	}

	slog.Info("stopped")
}
