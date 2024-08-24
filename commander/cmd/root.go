/*
Copyright © 2024 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"fortio.org/fortio/jrpc"
	"fortio.org/fortio/rapi"
	"fortio.org/log"
	"github.com/spf13/cobra"
)

var (
	outDir         *string
	clientListFile *string
	path           *string
	payloadFile    *string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "commander",
	Short: "A command and control system for fortio distributed load tests",
	Long: `Commander connects to clients specified in json format at the -c flag
	and posts the payload to each client at path. The intended use is to kick
	off a distributed load test using many fortio clients, and then retrieve the
	results, storing them in the output directory by client name.`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	RunE: func(cmd *cobra.Command, args []string) error {
		clients, err := readClients(*clientListFile)
		if err != nil {
			return err
		}

		payloadBytes := []byte{}
		if len(*payloadFile) > 0 {
			payloadfd, err := os.Open(*payloadFile)
			if err != nil {
				return err
			}
			payloadBytes, err = io.ReadAll(payloadfd)
			if err != nil {
				return err
			}
		}

		var merr error
		var wg sync.WaitGroup
		client := &http.Client{}
		ctx := context.Background()
		tc := TestClient{client: client, ctx: ctx}

		for name, uri_port := range clients {
			wg.Add(1)
			go func(uri_port, name string) {
				defer wg.Done()
				reply, err := tc.RunTestAsync("http://"+uri_port+*path, payloadBytes)
				if err != nil {
					merr = errors.Join(merr, err) // TODO: break
					return
				}
				for {
					responseBytes, err := tc.RetrieveResults(uri_port, reply)
					if err == nil {
						err = os.WriteFile(*outDir+name+".json", responseBytes, 0644)
						merr = errors.Join(merr, err)
						return
					}
					time.Sleep(1 * time.Second)
				}
			}(uri_port, name)
		}
		wg.Wait()
		return merr
	},
}

func readClients(clientListFile string) (map[string]string, error) {
	jsonFile, err := os.Open(clientListFile)
	if err != nil {
		return nil, err
	}
	byteValue, err := io.ReadAll(jsonFile)
	if err != nil {
		return nil, err
	}
	var clientsInflated []map[string]string
	err = json.Unmarshal(byteValue, &clientsInflated)
	if err != nil {
		return nil, err
	}
	clients := map[string]string{}
	for _, client := range clientsInflated {
		clients[client["name"]] = client["uri"]
	}
	return clients, nil
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.commander.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	outDir = rootCmd.Flags().StringP("outdir", "o", "", "Directory for storing output files")
	clientListFile = rootCmd.Flags().StringP("client-file", "c", "", "Yaml or json file containing key values of client name to client address")
	path = rootCmd.Flags().StringP("path", "p", "/fortio/rest/run", "HTTP path to send the start command to")
	payloadFile = rootCmd.Flags().StringP("payload", "d", "", "file containing the test payload to send (async should be on)")
}

type TestClient struct {
	client *http.Client
	ctx    context.Context
}

func (tc *TestClient) RunTestAsync(address string, payloadBytes []byte) (*rapi.AsyncReply, error) {
	req, err := http.NewRequestWithContext(tc.ctx, "POST", address, bytes.NewReader(payloadBytes))
	if err != nil {
		return nil, err
	}
	resp, err := tc.client.Do(req)
	if err != nil {
		return nil, err
	}
	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {

	}
	reply, err := jrpc.Deserialize[rapi.AsyncReply](responseBytes)
	if err != nil {
		return nil, err
	}
	if reply.RunID == 0 {
		err = errors.New("Got unexpected results when starting test")
		log.Errf("Got unexpected results when starting test: %v", string(responseBytes))
		return nil, err
	}
	return reply, nil
}

func (tc *TestClient) RetrieveResults(uri_port string, reply *rapi.AsyncReply) ([]byte, error) {
	req, err := http.NewRequestWithContext(tc.ctx, "GET", reply.ResultURL, nil)
	if err != nil {
		return nil, err
	}
	req.URL.Host = uri_port
	resp, err := tc.client.Do(req)
	if err != nil {
		return nil, err
	}

	responseBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Failed to retrieve results: %v", string(responseBytes))
	}
	return responseBytes, nil
}
