package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"path"
	"sync"
	"time"

	"github.com/northbright/csvhelper"
	"github.com/northbright/pathhelper"
)

var (
	participantsCSV   = `participants.csv`
	configFile        = `config.json`
	config            Config
	participants      []Participant
	availParticipants []Participant
	winnerMap         = map[int][]Participant{}
	cancelMap         = map[int]context.CancelFunc{}
	ctxMap            = map[int]context.Context{}
)

type Participant struct {
	ID   string
	Name string
}

type Prize struct {
	Name string `json:"name"`
	Num  int    `json:"num"`
}

type Blacklist struct {
	MaxPrizeIndex int      `json:"max_prize_index"`
	IDs           []string `json:"ids"`
}

type Config struct {
	Prizes     []Prize     `json:"prizes"`
	Blacklists []Blacklist `json:"blacklists"`
}

type Action struct {
	Name       string `json:"name"`
	PrizeIndex int    `json:"prize_index"`
}

type CommonResponse struct {
	Success bool   `json:"success"`
	ErrMsg  string `json:"err_msg"`
	Action  `json:"action"`
}

type PrizesResponse struct {
	CommonResponse
	Prizes []Prize `json:"prizes"`
}

type WinnersResponse struct {
	CommonResponse
	Winners []Participant `json:"winners"`
}

func loadParticipants(file string) ([]Participant, error) {
	currentDir, _ := pathhelper.GetCurrentExecDir()
	file = path.Join(currentDir, file)

	rows, err := csvhelper.ReadFile(file)
	if err != nil {
		return []Participant{}, err
	}

	var participants []Participant
	for _, row := range rows {
		if len(row) != 2 {
			return []Participant{}, fmt.Errorf("incorrect participants CSV")
		}
		participants = append(participants, Participant{row[0], row[1]})
	}
	return participants, nil

}

func loadConfig(file string, config *Config) error {
	currentDir, _ := pathhelper.GetCurrentExecDir()
	file = path.Join(currentDir, file)

	// Load Conifg.
	buf, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}

	return json.Unmarshal(buf, config)
}

func sendResponse(c *Client, res interface{}) error {
	buf, err := json.Marshal(res)
	if err != nil {
		return err
	}
	c.send <- buf
	return nil
}

func parseAction(message []byte) (Action, error) {
	action := Action{}

	if err := json.Unmarshal(message, &action); err != nil {
		return action, err
	}
	return action, nil
}

func processAction(c *Client, message []byte) {
	fmt.Printf("processAction() start, message: %s\n", message)

	mutex := &sync.Mutex{}

	action, err := parseAction(message)
	if err != nil {
		fmt.Printf("parseAction() error: %v\n", err)
		return
	}

	switch action.Name {
	case "get_prizes":
		if err = getPrizes(c, action); err != nil {
			fmt.Printf("getPrizes() error: %v\n", err)
		}

	case "get_winners":
		if err = getWinners(c, action, mutex); err != nil {
			fmt.Printf("getWinners() error: %v\n", err)
		}

	case "start":
		if _, ok := cancelMap[action.PrizeIndex]; ok {
			break
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancelMap[action.PrizeIndex] = cancel
		ctxMap[action.PrizeIndex] = ctx
		go start(ctx, c, action, mutex)

	case "stop":
		fmt.Printf("stop")
		cancel, ok := cancelMap[action.PrizeIndex]
		if !ok {
			break
		}
		cancel()
		ctx, ok := ctxMap[action.PrizeIndex]
		if ok {
			<-ctx.Done()
			delete(cancelMap, action.PrizeIndex)
		}
	}
}

func getPrizes(c *Client, a Action) error {
	commonRes := CommonResponse{Success: true, ErrMsg: "", Action: a}
	res := PrizesResponse{commonRes, config.Prizes}

	buf, err := json.Marshal(res)
	if err != nil {
		return err
	}
	c.send <- buf
	return nil
}

func getWinners(c *Client, a Action, mutex *sync.Mutex) error {
	mutex.Lock()
	defer mutex.Unlock()

	commonRes := CommonResponse{Success: true, ErrMsg: "", Action: a}

	winners := []Participant{}
	if _, ok := winnerMap[a.PrizeIndex]; ok {
		winners = winnerMap[a.PrizeIndex]
	}

	res := WinnersResponse{commonRes, winners}

	buf, err := json.Marshal(res)
	if err != nil {
		return err
	}
	c.send <- buf
	return nil
}

func start(ctx context.Context, c *Client, a Action, mutex *sync.Mutex) {
	var (
		err     error
		errMsg  = ""
		winners = []Participant{}
	)

	mutex.Lock()
	defer func() {
		mutex.Unlock()

		commonRes := CommonResponse{Success: false, ErrMsg: errMsg, Action: a}
		if errMsg == "" {
			commonRes.Success = true
		}

		res := WinnersResponse{commonRes, winners}
		if err = sendResponse(c, res); err != nil {
			fmt.Printf("sendResponse() err: %v\n", err)
		}
	}()

	// Check if there're already winners for current prize,
	// if so, re-get winners.
	oldWinners, ok := winnerMap[a.PrizeIndex]
	if ok {
		// Return old winners to available participants.
		availParticipants = append(availParticipants, winnerMap[a.PrizeIndex]...)
		fmt.Printf("return old winners to availParticipants\n")
		fmt.Printf("return winners: %v\n", oldWinners)
		fmt.Printf("after return winners, availParticipants: %v\n", availParticipants)
	}

	for {
		select {
		case <-ctx.Done():
			fmt.Printf("ctx.Done() in getWinners\n")
			// Modify action name when cancel() is called("stop" action received).
			a.Name = "stop"

			// Update winner map.
			winnerMap[a.PrizeIndex] = winners

			// Remove winners from available participants.
			availParticipants = removeWinners(availParticipants, winners)

			fmt.Printf("winners: %v\n", winners)
			fmt.Printf("after remove winners, availParticipants: %v\n", availParticipants)
			return
		default:
		}

		if winners, err = _getWinners(
			config.Prizes,
			a.PrizeIndex,
			availParticipants,
			config.Blacklists,
		); err != nil {
			errMsg = fmt.Sprintf("_getWinners() error: %v", err)
			fmt.Println(errMsg)
			return
		}

		commonRes := CommonResponse{Success: true, ErrMsg: errMsg, Action: a}
		res := WinnersResponse{commonRes, winners}

		if err = sendResponse(c, res); err != nil {
			fmt.Printf("sendResponse() err: %v\n", err)
		}

		time.Sleep(time.Millisecond * 100)
	}
}

func removeWinners(origin []Participant, winners []Participant) []Participant {
	var updated []Participant
	for _, p := range origin {
		found := false

		for _, w := range winners {
			if p.ID == w.ID {
				found = true
				break
			}
		}

		if !found {
			updated = append(updated, p)
		}
	}

	return updated
}

func getAvailParticipantsForPrize(prizeIndex int, origin []Participant, blacklists []Blacklist) ([]Participant, error) {
	if len(origin) <= 0 {
		return origin, nil
	}

	if len(blacklists) <= 0 {
		return origin, nil
	}

	var updated []Participant
	for _, p := range origin {
		found := false
		for _, blacklist := range blacklists {
			// Blacklist is not for current prize index.
			if prizeIndex <= blacklist.MaxPrizeIndex {
				continue
			}
			for _, ID := range blacklist.IDs {
				if p.ID == ID {
					found = true
					break
				}
			}
			if found {
				break
			}
		}
		if !found {
			updated = append(updated, p)
		}
	}
	return updated, nil
}

func _getWinners(prizes []Prize, prizeIndex int, availables []Participant, blacklists []Blacklist) ([]Participant, error) {

	prizesNum := len(prizes)
	if prizesNum <= 0 {
		return []Participant{}, fmt.Errorf("no prizes")
	}

	if prizeIndex < 0 || prizeIndex > prizesNum-1 {
		return []Participant{}, fmt.Errorf("prize index error")
	}

	n := prizes[prizeIndex].Num
	if n <= 0 {
		return []Participant{}, fmt.Errorf("no prizes for prize index: %v\n", prizeIndex)
	}

	m := len(availables)
	if m <= 0 {
		return []Participant{}, fmt.Errorf("no participants")
	}

	// Remove blacklists to update available participants.
	updatedAvailables, err := getAvailParticipantsForPrize(prizeIndex, availables, blacklists)
	if err != nil {
		return []Participant{}, fmt.Errorf("failed to get available participants for prize index: %v\n", prizeIndex)
	}

	// Check number of available participants.
	m = len(updatedAvailables)
	if m <= 0 {
		return []Participant{}, fmt.Errorf("no participants for prize index: %v\n", prizeIndex)
	}

	// Set current prize num to m(number of participants),
	// if number of participants is less than prize num:-).
	if m < n {
		n = m
	}

	var winners []Participant
	for i := 0; i < n; i++ {
		rand.Seed(time.Now().UnixNano())
		idx := rand.Intn(len(updatedAvailables))
		winners = append(winners, updatedAvailables[idx])
		// Update participants
		updatedAvailables = append(updatedAvailables[0:idx], updatedAvailables[idx+1:]...)
	}

	// Verify if there are duplicated winners.
	valid := verifyWinners(winners)
	if !valid {
		return []Participant{}, fmt.Errorf("invalid winners: %v", winners)
	}
	return winners, nil
}

func verifyWinners(winners []Participant) bool {
	m := map[string]Participant{}

	for _, p := range winners {
		if _, ok := m[p.ID]; ok {
			return false
		}
		m[p.ID] = p
	}
	return true
}

func round(prizeNum int, availables []Participant) ([]Participant, error) {
	winners := []Participant{}

	if prizeNum <= 0 {
		return winners, fmt.Errorf("incorrect prize number")
	}

	m := len(availables)
	if m <= 0 {
		return winners, fmt.Errorf("no participants")
	}

	// Set prize num to m(number of participants),
	// if number of participants is less than prize num:-).
	if m < prizeNum {
		prizeNum = m
	}

	for i := 0; i < prizeNum; i++ {
		rand.Seed(time.Now().UnixNano())
		idx := rand.Intn(len(availables))
		winners = append(winners, availables[idx])
		// Update participants
		availables = append(availables[0:idx], availables[idx+1:]...)
	}

	return winners, nil
}
