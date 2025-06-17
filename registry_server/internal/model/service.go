package model

type Service struct {
	Name          string // Name of the service
	Address       string // Address of the service
	LastHeartBeat int64  // Timestamp of the last heartbeat
}
