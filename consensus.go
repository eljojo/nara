package nara

import (
	"sort"
	"time"

	"github.com/sirupsen/logrus"
)

type consensusValue struct {
	value  int64
	weight uint64
}

type consensusCluster struct {
	values      []int64
	totalWeight uint64
}

func consensusByUptime(values []consensusValue, tolerance int64, allowCoinFlip bool) int64 {
	if len(values) == 0 {
		return 0
	}

	sort.Slice(values, func(i, j int) bool {
		return values[i].value < values[j].value
	})

	var clusters []consensusCluster
	current := consensusCluster{
		values:      []int64{values[0].value},
		totalWeight: values[0].weight,
	}
	clusterStart := values[0].value

	for i := 1; i < len(values); i++ {
		if values[i].value-clusterStart <= tolerance {
			current.values = append(current.values, values[i].value)
			current.totalWeight += values[i].weight
		} else {
			clusters = append(clusters, current)
			current = consensusCluster{
				values:      []int64{values[i].value},
				totalWeight: values[i].weight,
			}
			clusterStart = values[i].value
		}
	}
	clusters = append(clusters, current)

	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].totalWeight > clusters[j].totalWeight
	})

	for _, cluster := range clusters {
		if len(cluster.values) >= 2 {
			return cluster.values[len(cluster.values)/2]
		}
	}

	if allowCoinFlip && len(clusters) >= 2 {
		first := clusters[0]
		second := clusters[1]
		threshold := first.totalWeight * 80 / 100
		if second.totalWeight >= threshold {
			coin := time.Now().UnixNano() % 2
			if coin == 0 {
				logrus.Printf("🪙 coin flip! chose %d over %d (both had ~%d uptime)",
					first.values[0], second.values[0], first.totalWeight)
				return first.values[len(first.values)/2]
			}
			logrus.Printf("🪙 coin flip! chose %d over %d (both had ~%d uptime)",
				second.values[0], first.values[0], second.totalWeight)
			return second.values[len(second.values)/2]
		}
	}

	return clusters[0].values[len(clusters[0].values)/2]
}
