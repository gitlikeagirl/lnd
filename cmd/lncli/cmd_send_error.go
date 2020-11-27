package main

import (
	"context"
	"fmt"

	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/urfave/cli"
)

var sendErrorCommand = cli.Command{
	Name:  "senderror",
	Usage: "Send an error to a connected peer.",
	Description: `
	Sends an error to a connected peer. 

	senderror [pubkey] [message]
	`,
	Action: actionDecorator(sendError),
}

func sendError(ctx *cli.Context) error {
	client, cleanUp := getClient(ctx)
	defer cleanUp()

	if ctx.NArg() != 2 {
		return fmt.Errorf("peer pubkey and error arguments required")
	}

	pk, err := route.NewVertexFromStr(ctx.Args().Get(0))
	if err != nil {
		return fmt.Errorf("invalid pubkey: %v", err)
	}

	errMsg := ctx.Args().Get(1)
	if errMsg == "" {
		return fmt.Errorf("error message is required")
	}

	_, err = client.SendError(context.Background(), &lnrpc.SendErrorRequest{
		PeerPubkey:           pk[:],
		Error:                errMsg,
		XXX_NoUnkeyedLiteral: struct{}{},
		XXX_unrecognized:     nil,
		XXX_sizecache:        0,
	})
	if err != nil {
		return err
	}

	return nil
}
