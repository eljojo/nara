package main

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/kataras/tablewriter"
	"github.com/lensesio/tableprinter"
)

type neighbour struct {
	Name       string  `header:"name"`
	Ip         string  `header:"IP"`
	Ping       string  `header:"ping"`
	LastSeen   string  `header:"last seen"`
	Uptime     string  `header:"uptime"`
	FirstSeen  string  `header:"first seen"`
	Load       float64 `header:"load"`
	Restarts   int64   `header:"restarts"`
	Chattiness int64   `header:"chat"`
}

func printNeigbourhoodForever(refreshRate int) {
	for {
		printNeigbourhood()
		time.Sleep(time.Duration(refreshRate) * time.Second)
	}
}

func printNeigbourhood() {
	if len(neighbourhood) == 0 {
		return
	}

	printer := tableprinter.New(os.Stdout)
	naras := make([]neighbour, 0, len(neighbourhood)+1)

	nei := generateScreenRow(*me)
	naras = append(naras, nei)

	for _, nara := range neighbourhood {
		nei := generateScreenRow(nara)
		naras = append(naras, nei)
	}

	sort.Slice(naras, func(i, j int) bool {
		return naras[j].Name > naras[i].Name
	})

	// Optionally, customize the table, import of the underline 'tablewriter' package is required for that.
	printer.BorderTop, printer.BorderBottom, printer.BorderLeft, printer.BorderRight = true, true, true, true
	printer.CenterSeparator = "│"
	printer.ColumnSeparator = "│"
	printer.RowSeparator = "─"
	printer.HeaderBgColor = tablewriter.BgBlackColor
	printer.HeaderFgColor = tablewriter.FgGreenColor

	// Print the slice of structs as table, as shown above.
	printer.Print(naras)
}

func generateScreenRow(nara Nara) neighbour {
	now := time.Now().Unix()
	ping := ""
	if nara.Name != me.Name {
		ping = pingBetweenMs(*me, nara)
	}
	observation, _ := me.Status.Observations[nara.Name]
	lastSeen := timeAgoFriendly(now - observation.LastSeen)
	first_seen := timeAgoFriendly(now - observation.StartTime)
	uptime := timeDiffFriendly(observation.LastSeen - observation.LastRestart)
	if observation.Online != "ONLINE" {
		ping = observation.Online
	}
	loadAvg := nara.Status.HostStats.LoadAvg
	nei := neighbour{nara.Name, nara.Ip, ping, lastSeen, uptime, first_seen, loadAvg, observation.Restarts, nara.Status.Chattiness}
	return nei
}

func timeAgoFriendly(running_time int64) string {
	return fmt.Sprintf("%s ago", timeDiffFriendly(running_time))
}

func timeDiffFriendly(running_time int64) string {
	first_seen := ""
	if running_time > 86400 {
		first_seen = fmt.Sprintf("%d days", running_time/86400)
	} else if running_time > 3600 {
		first_seen = fmt.Sprintf("%d hours", running_time/3600)
	} else if running_time > 60 {
		first_seen = fmt.Sprintf("%d mins", running_time/60)
	} else {
		first_seen = fmt.Sprintf("%ds", running_time)
	}
	return first_seen
}
