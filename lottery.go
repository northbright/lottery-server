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
	ctx               context.Context
	cancel            context.CancelFunc = nil
	mutex                                = &sync.Mutex{}
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
	Name          string `json:"name"`
	PrizeIndex    int    `json:"prize_index"`
	WinnerIndexes []int  `json:"winner_indexes"`
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
	fmt.Printf("processAction()..., message: %s\n", message)

	//mutex := &sync.Mutex{}

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
		if cancel != nil {
			errMsg := fmt.Sprintf("start() is already running")
			sendWinnersResponse(c, action, []Participant{}, errMsg)
			fmt.Println(errMsg)
			break
		}

		prizeNum, updatedAvailables, err := getReady(
			action,
			config.Prizes,
			availParticipants,
			winnerMap,
			config.Blacklists,
		)

		if err != nil {
			errMsg := fmt.Sprintf("getReady() error: %v", err)
			sendWinnersResponse(c, action, []Participant{}, errMsg)
			fmt.Println(errMsg)
			break
		}

		ctx, cancel = context.WithCancel(context.Background())
		go start(ctx, c, action, prizeNum, updatedAvailables, mutex)

	case "stop":
		fmt.Printf("stop\n")
		if cancel == nil {
			errMsg := fmt.Sprintf("no start is running for prize: %v", action.PrizeIndex)
			sendWinnersResponse(c, action, []Participant{}, errMsg)
			break
		}
		cancel()
		<-ctx.Done()
		// Set cancel to nil
		cancel = nil
		fmt.Printf("stop <- ctx.Done()\n")
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

func start(ctx context.Context, c *Client, a Action, prizeNum int, availables []Participant, mutex *sync.Mutex) {
	var (
		err     error
		errMsg  = ""
		winners = []Participant{}
	)

	mutex.Lock()
	defer func() {
		mutex.Unlock()
		sendWinnersResponse(c, a, winners, errMsg)
	}()

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

		/*
			if winners, err = _getWinners(
				config.Prizes,
				a.PrizeIndex,
				availParticipants,
				config.Blacklists,
			); err != nil {
				errMsg = fmt.Sprintf("_getWinners() error: %v", err)
				fmt.Println(errMsg)
				return
			}*/

		/*
			if winners, err = lottery(
				a,
				config.Prizes,
				availParticipants,
				oldWinners,
				config.Blacklists,
			); err != nil {
				errMsg = fmt.Sprintf("_getWinners() error: %v", err)
				fmt.Println(errMsg)
				return
			}
		*/

		winners, availables, err = round(prizeNum, availables, winners)
		if err != nil {
			errMsg = fmt.Sprintf("rount() error: %v", err)
			fmt.Println(errMsg)
			return
		}

		/*
			for i, p := range winners {
				fmt.Printf("%v: ID: %v, Name: %v\n", i, p.ID, p.Name)
			}
		*/
		sendWinnersResponse(c, a, winners, errMsg)
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

/*
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
*/

/*
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
*/

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

func round(prizeNum int, availables []Participant, oldWinners []Participant) ([]Participant, []Participant, error) {
	winners := []Participant{}

	// Append older winners to available participants firstly.
	// For the first time, oldWinners is empty.
	availables = append(availables, oldWinners...)

	if prizeNum <= 0 {
		return winners, availables, fmt.Errorf("incorrect prize number")
	}

	m := len(availables)
	if m <= 0 {
		return winners, availables, fmt.Errorf("no participants")
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

	valid := verifyWinners(winners)
	if !valid {
		return []Participant{}, availables, fmt.Errorf("invalid winners: %v", winners)
	}

	return winners, availables, nil
}

func getBlacklistIDs(blacklists []Blacklist, prizeIndex int) map[string]string {
	m := map[string]string{}

	if len(blacklists) <= 0 {
		return m
	}

	for _, blacklist := range blacklists {
		// Prizes are sorted by level(DESC order).
		// Blacklist.MaxPrizeIndex means:
		// the participants can not participate the lottery which prize index > MaxPrizeIndex.
		// e.g.
		// "prizes": [ {"name":"3rd prize", "num": 10}, {"name":"2nd prize", "num": 5}, {"name":"1st prize", "num": 1} ]
		// "blacklists": [ {"max_prize_index":1, "ids": "0"} ]
		// participants csv:
		// 1,Frank
		// 2,Bob
		// 3,Tom
		// ......
		// It means "Frank" and "Bob" can only paticipate "3rd prize"(index = 0) and "2nd prize"(index = 1).
		if prizeIndex <= blacklist.MaxPrizeIndex {
			continue
		}

		for _, ID := range blacklist.IDs {
			m[ID] = ID
		}
	}

	return m
}

func removeBlacklist(origin []Participant, blacklist map[string]string) []Participant {
	updated := []Participant{}

	for _, p := range origin {
		found := false
		for ID, _ := range blacklist {
			if p.ID == ID {
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

func needLottery(oldWinners []Participant, winnerIndexes []int) (bool, error) {
	nOldWinners := len(oldWinners)

	if nOldWinners == 0 {
		// Winner indexes are not empty(want to re-lottery) for 1st time lottery.
		if len(winnerIndexes) != 0 {
			return false, fmt.Errorf("1st time to lottery, invalid returned winner indexes")
		}
		// 1st time lottery.
		return true, nil
	} else {
		// Older winners are not empty, it's re-lottery but with no winner indexes.
		if len(winnerIndexes) == 0 {
			return false, nil
		}

		// Check if winner indexes are valid.
		for _, idx := range winnerIndexes {
			if idx < 0 || idx >= nOldWinners {
				return false, fmt.Errorf("invalid returned winner index: %v\n", idx)
			}
		}
		// Re-lottery
		return true, nil
	}
}

func returnWinners(origin []Participant, oldWinners []Participant, winnerIndexes []int) []Participant {
	l := len(oldWinners)
	if l <= 0 {
		return origin
	}

	updated := origin
	for _, idx := range winnerIndexes {
		if idx < 0 || idx >= l {
			continue
		}

		updated = append(updated, oldWinners[idx])
		fmt.Printf("return winners: %v\n", oldWinners[idx])
	}
	return updated
}

func getUpdatedAvailableParticipants(a Action, origin []Participant, oldWinners []Participant, blacklists []Blacklist) []Participant {

	updatedAvailables := origin
	if len(a.WinnerIndexes) > 0 {
		updatedAvailables = returnWinners(updatedAvailables, oldWinners, a.WinnerIndexes)
	}

	prizeIndex := a.PrizeIndex
	blacklistIDs := getBlacklistIDs(blacklists, prizeIndex)
	updatedAvailables = removeBlacklist(updatedAvailables, blacklistIDs)
	return updatedAvailables
}

/*
func lottery(a Action, prizes []Prize, availables []Participant, oldWinners []Participant, blacklists []Blacklist) ([]Participant, error) {
	if len(prizes) <= 0 {
		return []Participant{}, fmt.Errorf("no prizes")
	}

	prizeIndex := a.PrizeIndex
	if prizeIndex < 0 || prizeIndex >= len(prizes) {
		return []Participant{}, fmt.Errorf("prize index error")
	}

	prizeNum := prizes[prizeIndex].Num
	if prizeNum <= 0 {
		return []Participant{}, fmt.Errorf("no prizes for prize index: %v\n", prizeIndex)
	}

	updatedAvailables := getUpdatedAvailableParticipants(a, availables, oldWinners, blacklists)

	if len(updatedAvailables) <= 0 {
		return []Participant{}, fmt.Errorf("no available participants")
	}

	return round(prizeNum, updatedAvailables)
}
*/

func sendWinnersResponse(c *Client, a Action, winners []Participant, errMsg string) {
	success := true
	if errMsg != "" {
		success = false
	}

	commonRes := CommonResponse{Success: success, ErrMsg: errMsg, Action: a}
	res := WinnersResponse{CommonResponse: commonRes, Winners: winners}
	sendResponse(c, res)
}

func getReady(action Action, prizes []Prize, availables []Participant, winnerMap map[int][]Participant, blacklists []Blacklist) (int, []Participant, error) {
	// Check if there're already winners for current prize,
	// if so, re-get winners.
	oldWinners := []Participant{}
	if _, ok := winnerMap[action.PrizeIndex]; ok {
		oldWinners = winnerMap[action.PrizeIndex]

		// Return old winners to available participants.
		//availParticipants = append(availParticipants, winnerMap[a.PrizeIndex]...)
		//fmt.Printf("return old winners to availParticipants\n")
		//fmt.Printf("return winners: %v\n", oldWinners)
		//fmt.Printf("after return winners, availParticipants: %v\n", availParticipants)
	}

	need, err := needLottery(oldWinners, action.WinnerIndexes)
	if err != nil {
		return 0, []Participant{}, fmt.Errorf("needLottery() error: %v\n", err)
	}

	if !need {
		return 0, []Participant{}, fmt.Errorf("no need")
	}

	fmt.Printf("need = true\n")

	if len(prizes) <= 0 {
		return 0, []Participant{}, fmt.Errorf("no prizes")
	}

	prizeIndex := action.PrizeIndex
	if prizeIndex < 0 || prizeIndex >= len(prizes) {
		return 0, []Participant{}, fmt.Errorf("prize index error")
	}

	prizeNum := prizes[prizeIndex].Num
	if prizeNum <= 0 {
		return 0, []Participant{}, fmt.Errorf("no prizes for prize index: %v\n", prizeIndex)
	}

	updatedAvailables := getUpdatedAvailableParticipants(action, availables, oldWinners, blacklists)

	if len(updatedAvailables) <= 0 {
		return 0, []Participant{}, fmt.Errorf("no available participants")
	}

	return prizeNum, updatedAvailables, nil
}
