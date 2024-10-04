/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"errors"
	"fmt"
	"math"
	"os"

	"fortio.org/fortio/fhttp"
	"fortio.org/fortio/jrpc"
	"fortio.org/fortio/stats"
	"github.com/spf13/cobra"
)

// aggregateCmd represents the aggregate command
var aggregateCmd = &cobra.Command{
	Use:   "aggregate",
	Short: "an aggregator for fortio load test results",
	Long: `Usage:
	commander aggregate /path/to/results`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		resultPath := args[0]
		dir, err := os.ReadDir(resultPath)
		if err != nil {
			return err
		}
		var merr error
		var outRes *fhttp.HTTPRunnerResults
		// outRes := fhttp.HTTPRunnerResults{
		// 	RunnerResults: periodic.RunnerResults{
		// 		StartTime: time.Now().Add(time.Hour * 24 * 365), // max time, will be overwritten
		// 	},
		// }
		outDur := stats.NewHistogram(0, 0.0001)
		outEDur := stats.NewHistogram(0, 0.0001)
		outConnStats := stats.NewHistogram(0, 0.0001)
		outSizes := stats.NewHistogram(0, 0.0001)
		outHSizes := stats.NewHistogram(0, 0.0001)
		for _, entry := range dir {
			if entry.IsDir() {
				continue
			}
			fbytes, err := os.ReadFile(resultPath + "/" + entry.Name())
			if err != nil {
				merr = errors.Join(merr, err)
			}
			res, err := jrpc.Deserialize[fhttp.HTTPRunnerResults](fbytes)
			if err != nil {
				merr = errors.Join(merr, err)
			}
			if res.Exception != "" {
				merr = errors.Join(merr, fmt.Errorf("Failed test at %v: %v", entry.Name(), res.Exception))
				continue
			}
			if outRes == nil {
				outRes = res
			} else {
				outRes = merge(outRes, res)
			}
			mergeHistograms(outDur, res.DurationHistogram)
			mergeHistograms(outEDur, res.ErrorsDurationHistogram)
			mergeHistograms(outConnStats, res.ConnectionStats)
			mergeHistograms(outSizes, res.Sizes)
			mergeHistograms(outHSizes, res.HeaderSizes)
		}
		pctiles, _ := stats.ParsePercentiles("50,+75,+90,+99,+99.9")
		outRes.DurationHistogram = outDur.Export().CalcPercentiles(pctiles)
		outRes.ErrorsDurationHistogram = outEDur.Export().CalcPercentiles(pctiles)
		outRes.ConnectionStats = outConnStats.Export().CalcPercentiles(pctiles)
		outRes.Sizes = outSizes.Export().CalcPercentiles(pctiles)
		outRes.HeaderSizes = outHSizes.Export().CalcPercentiles(pctiles)
		outBytes, err := jrpc.Serialize(outRes)
		fmt.Println(string(outBytes))
		return merr
	},
}

// merge aggregates the non-histogram results of two test runs
func merge(a, b *fhttp.HTTPRunnerResults) *fhttp.HTTPRunnerResults {
	a.ActualQPS += b.ActualQPS
	a.NumConnections += b.NumConnections
	a.NumThreads += b.NumThreads

	// duration should be the superset of the two time windows (assumes overlapping)
	if a.StartTime.Add(a.ActualDuration).Before(b.StartTime.Add(b.ActualDuration)) {
		a.ActualDuration = b.StartTime.Add(b.ActualDuration).Sub(a.StartTime)
	}
	if a.StartTime.After(b.StartTime) {
		a.StartTime = b.StartTime
	}

	// TODO: merge RetCodes
	return a
}

func mergeHistograms(hist *stats.Histogram, data *stats.HistogramData) error {
	for _, bucket := range data.Data {
		middle := (bucket.End + bucket.Start) / 2
		if bucket.Count > math.MaxInt {
			hist.RecordN(middle, math.MaxInt)
			bucket.Count -= math.MaxInt
		}
		hist.RecordN(middle, int(bucket.Count))
	}
	return nil
}

// func mergeHist(a, b *stats.HistogramData) error {
// 	aHash := map[stats.Interval]*stats.Bucket{}
// 	for _, bucket := range a.Data {
// 		aHash[bucket.Interval] = &bucket
// 		bucket.Percent = 0
// 	}
// 	for _, bucket := range b.Data {
// 		if _, ok := aHash[bucket.Interval]; ok {
// 			aHash[bucket.Interval].Count += bucket.Count
// 		} else {
// 			a.Data = append(a.Data, bucket)
// 		}
// 	}
// 	for k, v := range b {
// 		a[k] += v
// 	}
// 	return a
// }

func init() {
	rootCmd.AddCommand(aggregateCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// aggregateCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// aggregateCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
