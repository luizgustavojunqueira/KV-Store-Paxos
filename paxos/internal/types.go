package internal

type PaxosState struct {
	highestPromisedID  int64
	acceptedProposedID int64
	acceptedValue      []byte
}
