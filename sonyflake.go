// Package sonyflake implements Sonyflake, a distributed unique ID generator inspired by Twitter's Snowflake.
//
// A Sonyflake ID is composed of
//
//	39 bits for time in units of 10 msec
//	 8 bits for a sequence number
//	16 bits for a machine id
package sonyflake

import (
	"errors"
	"sync"
	"time"
	"net"
)

// These constants are the bit lengths of Sonyflake ID parts.
const (
	BitLenTime      = 42 // bit length of time
	BitLenSequence  = 12 // bit length of sequence number
	BitLenNodeID    = 10 // bit length of machine id
)

// Settings configures Sonyflake:
//
// StartTime is the time since which the Sonyflake time is defined as the elapsed time.
// If StartTime is 0, the start time of the Sonyflake is set to "2014-09-01 00:00:00 +0000 UTC".
// If StartTime is ahead of the current time, Sonyflake is not created.
//
// NodeID returns the unique ID of the Sonyflake instance.
// If NodeID returns an error, Sonyflake is not created.
// If NodeID is nil, default NodeID is used.
// Default NodeID returns the lower 16 bits of the private IP address.
//
// CheckNodeID validates the uniqueness of the machine ID.
// If CheckNodeID returns false, Sonyflake is not created.
// If CheckNodeID is nil, no validation is done.
type Settings struct {
	StartTime      time.Time
	NodeID      func() (uint64, error)
	CheckNodeID func(uint64) bool
}

// Sonyflake is a distributed unique ID generator.
type Sonyflake struct {
	mutex       *sync.Mutex
	startTime   int64
	elapsedTime int64
	sequence    uint64
	nodeID   	uint64
}

// NewSonyflake returns a new Sonyflake configured with the given Settings.
// NewSonyflake returns nil in the following cases:
// - Settings.StartTime is ahead of the current time.
// - Settings.NodeID returns an error.
// - Settings.CheckNodeID returns false.
func NewSonyflake(st Settings) *Sonyflake {
	sf := new(Sonyflake)
	sf.mutex = new(sync.Mutex)
	sf.sequence = uint64(1<<BitLenSequence - 1)

	if st.StartTime.After(time.Now()) {
		return nil
	}
	if st.StartTime.IsZero() {
		sf.startTime = toSonyflakeTime(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	} else {
		sf.startTime = toSonyflakeTime(st.StartTime)
	}

	var err error
	//if st.NodeID == nil {
		//sf.nodeID, err = uint64(0), nil
		sf.nodeID, err = Lower64BitPrivateIP()
	// else {
	//	sf.nodeID, err = st.NodeID()
	//}
	if err != nil || (st.CheckNodeID != nil && !st.CheckNodeID(sf.nodeID)) {
		return nil
	}

	return sf
}

// NextID generates a next unique ID.
// After the Sonyflake time overflows, NextID returns an error.
func (sf *Sonyflake) NextID() (uint64, error) {
	const maskSequence = uint64(1<<BitLenSequence - 1)

	sf.mutex.Lock()
	defer sf.mutex.Unlock()

	current := currentElapsedTime(sf.startTime)
	if sf.elapsedTime < current {
		sf.elapsedTime = current
		sf.sequence = 0
	} else { // sf.elapsedTime >= current
		sf.sequence = (sf.sequence + 1) & maskSequence
		if sf.sequence == 0 {
			sf.elapsedTime++
			overtime := sf.elapsedTime - current
			time.Sleep(sleepTime((overtime)))
		}
	}

	return sf.toID()
}

const sonyflakeTimeUnit = 1e7 // nsec, i.e. 10 msec

func toSonyflakeTime(t time.Time) int64 {
	return t.UTC().UnixNano() / sonyflakeTimeUnit
}

func currentElapsedTime(startTime int64) int64 {
	return toSonyflakeTime(time.Now()) - startTime
}

func sleepTime(overtime int64) time.Duration {
	return time.Duration(overtime*sonyflakeTimeUnit) -
		time.Duration(time.Now().UTC().UnixNano()%sonyflakeTimeUnit)
}

func (sf *Sonyflake) toID() (uint64, error) {
	if sf.elapsedTime >= 1<<BitLenTime {
		return 0, errors.New("over the time limit")
	}

	return uint64(sf.elapsedTime)<<(BitLenSequence+BitLenNodeID) |
		uint64(sf.sequence)<<BitLenNodeID |
		uint64(sf.nodeID), nil
}

func privateIPv4() (net.IP, error) {
	as, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}

	for _, a := range as {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}

		ip := ipnet.IP.To4()
		if isPrivateIPv4(ip) {
			return ip, nil
		}
	}
	return nil, errors.New("no private ip address")
}

func isPrivateIPv4(ip net.IP) bool {
	return ip != nil &&
		(ip[0] == 10 || ip[0] == 172 && (ip[1] >= 16 && ip[1] < 32) || ip[0] == 192 && ip[1] == 168)
}

func Lower64BitPrivateIP() (uint64, error) {
	ip, err := privateIPv4()
	if err != nil {
		return 0, err
	}

	return uint64(ip[2])<<8 + uint64(ip[3]), nil
}

// ElapsedTime returns the elapsed time when the given Sonyflake ID was generated.
func ElapsedTime(id uint64) time.Duration {
	return time.Duration(elapsedTime(id) * sonyflakeTimeUnit)
}

func elapsedTime(id uint64) uint64 {
	return id >> (BitLenSequence + BitLenNodeID)
}

// SequenceNumber returns the sequence number of a Sonyflake ID.
func SequenceNumber(id uint64) uint64 {
	const maskSequence = uint64((1<<BitLenSequence - 1) << BitLenNodeID)
	return id & maskSequence >> BitLenNodeID
}

// NodeID returns the machine ID of a Sonyflake ID.
func NodeID(id uint64) uint64 {
	const maskNodeID = uint64(1<<BitLenNodeID - 1)
	return id & maskNodeID
}

// Decompose returns a set of Sonyflake ID parts.
func Decompose(id uint64) map[string]uint64 {
	msb := id >> 64
	time := elapsedTime(id)
	sequence := SequenceNumber(id)
	NodeID := NodeID(id)
	return map[string]uint64{
		"id":         id,
		"msb":        msb,
		"time":       time,
		"node":    NodeID,
		"sequence":   sequence,
	}
}
