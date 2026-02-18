package dm_test

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	dm "github.com/MatchaCake/bilibili_dm_lib"
)

func Example_callback() {
	// Create a client for room 510 (short ID is resolved automatically).
	client := dm.NewClient(
		dm.WithRoomID(510),
	)

	// Register typed callbacks.
	client.OnDanmaku(func(d *dm.Danmaku) {
		fmt.Printf("[弹幕] %s: %s\n", d.Sender, d.Content)
	})

	client.OnGift(func(g *dm.Gift) {
		fmt.Printf("[礼物] %s %s %s x%d\n", g.User, g.Action, g.GiftName, g.Num)
	})

	client.OnSuperChat(func(sc *dm.SuperChat) {
		fmt.Printf("[SC ¥%d] %s: %s\n", sc.Price, sc.User, sc.Message)
	})

	// Start blocks until context is cancelled.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := client.Start(ctx); err != nil && ctx.Err() == nil {
		fmt.Println("error:", err)
	}
}

func Example_channelSubscribe() {
	client := dm.NewClient(
		dm.WithRoomID(21452505),
	)

	// Channel-based subscription receives all events.
	events := client.Subscribe()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := client.Start(ctx); err != nil && ctx.Err() == nil {
			fmt.Println("error:", err)
		}
	}()

	// Process events from the channel.
	for ev := range events {
		switch d := ev.Data.(type) {
		case *dm.Danmaku:
			fmt.Printf("[%d] %s: %s\n", ev.RoomID, d.Sender, d.Content)
		case *dm.Gift:
			fmt.Printf("[%d] Gift: %s x%d from %s\n", ev.RoomID, d.GiftName, d.Num, d.User)
		}
	}
}

func Example_multiRoom() {
	// Subscribe to multiple rooms at once.
	client := dm.NewClient(
		dm.WithRoomID(510),
		dm.WithRoomID(21452505),
	)

	client.OnDanmaku(func(d *dm.Danmaku) {
		fmt.Printf("%s: %s\n", d.Sender, d.Content)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	_ = client.Start(ctx)
}

func Example_authenticated() {
	// With cookies, you receive richer danmaku data (full medal info, etc.)
	client := dm.NewClient(
		dm.WithRoomID(510),
		dm.WithCookie("your_SESSDATA", "your_bili_jct"),
	)

	client.OnDanmaku(func(d *dm.Danmaku) {
		medal := ""
		if d.MedalName != "" {
			medal = fmt.Sprintf("[%s %d] ", d.MedalName, d.MedalLevel)
		}
		fmt.Printf("%s%s: %s\n", medal, d.Sender, d.Content)
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	_ = client.Start(ctx)
}
