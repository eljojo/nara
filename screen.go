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
	now := time.Now().Unix()

	printer := tableprinter.New(os.Stdout)
	naras := make([]neighbour, 0, len(neighbourhood)+1)

	observation, _ := me.Status.Observations[me.Name]
	lastSeen := fmt.Sprintf("%ds ago", now-observation.LastSeen)
	uptime := fmt.Sprintf("%ds", observation.LastSeen-observation.StartTime)
	nei := neighbour{me.Name, me.Ip, "", lastSeen, uptime, me.Status.HostStats.LoadAvg, observation.Restarts, me.Status.Chattiness}
	naras = append(naras, nei)

	for _, nara := range neighbourhood {
		ping := pingBetweenMs(*me, nara)
		observation, _ := me.Status.Observations[nara.Name]
		lastSeen := fmt.Sprintf("%ds ago", now-observation.LastSeen)
		uptime := fmt.Sprintf("%ds", observation.LastSeen-observation.StartTime)
		if observation.Online != "ONLINE" {
			ping = observation.Online
		}
		loadAvg := nara.Status.HostStats.LoadAvg
		nei := neighbour{nara.Name, nara.Ip, ping, lastSeen, uptime, loadAvg, observation.Restarts, nara.Status.Chattiness}
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
