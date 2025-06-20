package internal

import pb "github.com/luizgustavojunqueira/KV-Store-Paxos/proto/paxos"

type PaxosState struct {
	HighestPromisedID  int64
	AcceptedProposedID int64
	AcceptedCommand    *pb.Command
}

type LeaderPaxosState struct {
	HighestPromisedID  int64
	AcceptedProposedID int64
	AcceptedLeaderAddr string
}
