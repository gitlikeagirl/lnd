package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"strconv"

	"github.com/urfave/cli"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/signal"
)

var acceptorCommand = cli.Command{
	Name: "acceptchans",
	Description: `
This is a test command to test the channel acceptor. 
It will call the channel acceptance endpoint and then 
request input from you every time a new channel open 
request is produced. 

By default the channel will be accepted. If you give
the wrong input, you may just be ignored. It kinda works.
`,
	Action: actionDecorator(acceptChannels),
}

func acceptChannels(ctx *cli.Context) error {
	if err := signal.Intercept(); err != nil {
		return err
	}

	client, cleanUp := getClient(ctx)
	defer cleanUp()

	acceptCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	acceptClient, err := client.ChannelAcceptor(acceptCtx)
	if err != nil {
		return err
	}

	// Quit is closed if we fail to receive from lnd.
	quit := make(chan struct{})

	// We pipe requests in here
	requests := make(chan *lnrpc.ChannelAcceptRequest)

	// Since receives block, we run a goroutine to pipe them into a channel.
	// This allows us to sigterm the goroutine while we're waiting for a
	// channel accept request. We cancel context on exit, do this should
	// terminate too.
	go func() {
		defer close(quit)
		for {
			request, err := acceptClient.Recv()
			if err != nil {
				if err != context.Canceled {
					fmt.Println("receive err: ", err)
				}
				return
			}

			select {
			case requests <- request:
			case <-signal.ShutdownChannel():
				return
			}
		}
	}()

	for {
		fmt.Println("Waiting for channel open requests")
		// Our receive blocks, so we check whether we want to kill the
		// stream before we try to read from it.
		var request *lnrpc.ChannelAcceptRequest
		select {
		case request = <-requests:

		case <-signal.ShutdownChannel():
			return nil

		case <-quit:
			return nil
		}

		// Create a default response.
		acceptanceResp := &lnrpc.ChannelAcceptResponse{
			Accept:        true,
			PendingChanId: request.PendingChanId,
		}

		fmt.Printf("Accept channel %v?\n",
			hex.EncodeToString(request.PendingChanId))

		var answer string
		fmt.Scanln(&answer)

		if answer == "n" {
			acceptanceResp.Accept = false

			fmt.Println("Custom error for rejected chan?")
			fmt.Scanln(&answer)
			acceptanceResp.Error = answer
		}

		fmt.Println("Set more fields?")
		fmt.Scanln(&answer)

		if answer == "y" {
			fmt.Println("Upfront shutdown addr?")
			fmt.Scanln(&acceptanceResp.UpfrontShutdown)

			answer = "0"
			fmt.Println("chan_reserve_sat?")
			fmt.Scanln(&answer)

			reserve, err := strconv.ParseUint(answer, 10, 64)
			if err != nil {
				return err
			}
			acceptanceResp.ReserveSat = reserve

			answer = "0"
			fmt.Println("max_pending_amt_msat?")
			fmt.Scanln(&answer)

			max, err := strconv.ParseUint(answer, 10, 64)
			if err != nil {
				return err
			}
			acceptanceResp.InFlightMaxMsat = max

			answer = "0"
			fmt.Println("max_accepted_htlcs?")
			fmt.Scanln(&answer)

			htlcs, err := strconv.ParseUint(answer, 10, 32)
			if err != nil {
				return err
			}
			acceptanceResp.MaxHtlcCount = uint32(htlcs)

			answer = "0"
			fmt.Println("min_htlc_msat?")
			fmt.Scanln(&answer)

			minIn, err := strconv.ParseUint(answer, 10, 64)
			if err != nil {
				return err
			}
			acceptanceResp.MinHtlcIn = minIn

			answer = "0"
			fmt.Println("csv_delay?")
			fmt.Scanln(&answer)
			csv, err := strconv.ParseUint(answer, 10, 32)
			if err != nil {
				return err
			}
			acceptanceResp.CsvDelay = uint32(csv)

			answer = "0"
			fmt.Println("required confirmations?")
			fmt.Scanln(&answer)
			minDep, err := strconv.ParseUint(answer, 10, 32)
			if err != nil {
				return err
			}

			acceptanceResp.MinAcceptDepth = uint32(minDep)
		}

		if err := acceptClient.Send(acceptanceResp); err != nil {
			return err
		}
	}
}
