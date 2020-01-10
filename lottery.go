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
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Prize struct {
	Name    string `json:"name"`
	Num     int    `json:"num"`
	Content string `json:"content"`
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
	Name             string `json:"name"`
	PrizeIndex       int    `json:"prize_index"`
	OldWinnerIndexes []int  `json:"old_winner_indexes"`
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

		/*
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

		*/
		if err := validate(config.Prizes, action.PrizeIndex, winnerMap[action.PrizeIndex], action.OldWinnerIndexes); err != nil {
			errMsg := fmt.Sprintf("validate() error: %v", err)
			sendWinnersResponse(c, action, []Participant{}, errMsg)
			fmt.Println(errMsg)
			break
		}

		prizeNum, err := getPrizeNum(config.Prizes, action.PrizeIndex, action.OldWinnerIndexes)
		if err != nil {
			errMsg := fmt.Sprintf("getPrizeNum() error: %v", err)
			sendWinnersResponse(c, action, []Participant{}, errMsg)
			fmt.Println(errMsg)
			break
		}

		/*
			availParticipants = getUpdatedAvailableParticipants(action, availParticipants, winnerMap[action.PrizeIndex], config.Blacklists)

			if len(availParticipants) <= 0 {
				errMsg := fmt.Sprintf("no available participants")
				sendWinnersResponse(c, action, []Participant{}, errMsg)
				fmt.Println(errMsg)
			}
		*/

		// Return older winners for re-lottery.
		returnedWinners := getReturnedWinners(winnerMap[action.PrizeIndex], action.OldWinnerIndexes)
		availParticipants = append(availParticipants, returnedWinners...)

		updatedAvailables := getAvailableParticipantsAfterRemovedBlacklist(
			action.PrizeIndex,
			availParticipants,
			config.Blacklists)

		fmt.Printf("updatedAvailables: %v\n", updatedAvailables)

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
	//mutex.Lock()
	//defer mutex.Unlock()

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

			// If old winners and old winner indexes(want to re-lottery) are not empty.
			// count of new winners = len(old winner indexes)
			if len(winnerMap[a.PrizeIndex]) > 0 && len(a.OldWinnerIndexes) > 0 {
				if len(winners) != len(a.OldWinnerIndexes) {
					errMsg = fmt.Sprintf("len(action.OldWinnerIndexes) != len(winners)")
					fmt.Println(errMsg)
					return
				}

				for i, idx := range a.OldWinnerIndexes {
					winnerMap[a.PrizeIndex][idx] = winners[i]
				}
			} else { // Old winners are empty, 1st lottery
				winnerMap[a.PrizeIndex] = winners
			}

			fmt.Printf("winners: %v\n", winners)
			fmt.Printf("before remove winners, availParticipants: %v\n", availParticipants)

			// Remove winners from available participants.
			availParticipants = removeWinners(availParticipants, winners)

			fmt.Printf("after remove winners, availParticipants: %v\n", availParticipants)
			return
		default:
		}

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

func needLottery(oldWinners []Participant, oldWinnerIndexes []int) (bool, error) {
	nOldWinners := len(oldWinners)

	if nOldWinners == 0 {
		// Winner indexes are not empty(want to re-lottery) for 1st time lottery.
		if len(oldWinnerIndexes) != 0 {
			return false, fmt.Errorf("1st time to lottery, invalid returned winner indexes")
		}
		// 1st time lottery.
		return true, nil
	} else {
		// Older winners are not empty, it's re-lottery but with no winner indexes.
		if len(oldWinnerIndexes) == 0 {
			return false, nil
		}

		// Check if winner indexes are valid.
		for _, idx := range oldWinnerIndexes {
			if idx < 0 || idx >= nOldWinners {
				return false, fmt.Errorf("invalid returned winner index: %v\n", idx)
			}
		}
		// Re-lottery
		return true, nil
	}
}

func getReturnedWinners(oldWinners []Participant, oldWinnerIndexes []int) []Participant {
	returnedWinners := []Participant{}

	l := len(oldWinners)
	if l <= 0 {
		return returnedWinners
	}

	if len(oldWinnerIndexes) <= 0 {
		return returnedWinners
	}

	for _, idx := range oldWinnerIndexes {
		if idx < 0 || idx >= l {
			continue
		}

		returnedWinners = append(returnedWinners, oldWinners[idx])
		fmt.Printf("return winner: %v\n", oldWinners[idx])
	}
	return returnedWinners
}

func getAvailableParticipantsAfterRemovedBlacklist(prizeIndex int, origin []Participant, blacklists []Blacklist) []Participant {

	/*
		updatedAvailables := origin
		if len(a.OldWinnerIndexes) > 0 {
			updatedAvailables = returnWinners(updatedAvailables, oldWinners, a.OldWinnerIndexes)
		}
	*/

	updatedAvailables := origin
	blacklistIDs := getBlacklistIDs(blacklists, prizeIndex)
	updatedAvailables = removeBlacklist(updatedAvailables, blacklistIDs)
	return updatedAvailables
}

func sendWinnersResponse(c *Client, a Action, winners []Participant, errMsg string) {
	success := true
	if errMsg != "" {
		success = false
	}

	commonRes := CommonResponse{Success: success, ErrMsg: errMsg, Action: a}
	res := WinnersResponse{CommonResponse: commonRes, Winners: winners}
	sendResponse(c, res)
}

/*
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

	need, err := needLottery(oldWinners, action.OldWinnerIndexes)
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

	prizeNum := 0
	if len(action.OldWinnerIndexes) > 0 {
		prizeNum = len(action.OldWinnerIndexes)
	} else {
		prizeNum = prizes[prizeIndex].Num
	}

	if prizeNum <= 0 {
		return 0, []Participant{}, fmt.Errorf("no prizes for prize index: %v\n", prizeIndex)
	}

	updatedAvailables := getUpdatedAvailableParticipants(action, availables, oldWinners, blacklists)

	if len(updatedAvailables) <= 0 {
		return 0, []Participant{}, fmt.Errorf("no available participants")
	}

	return prizeNum, updatedAvailables, nil
}
*/

func validate(prizes []Prize, prizeIndex int, oldWinners []Participant, oldWinnerIndexes []int) error {
	// Check if there're already winners for current prize,
	// if so, re-get winners.
	/*oldWinners := []Participant{}
	if _, ok := winnerMap[action.PrizeIndex]; ok {
		oldWinners = winnerMap[action.PrizeIndex]

		// Return old winners to available participants.
		//availParticipants = append(availParticipants, winnerMap[a.PrizeIndex]...)
		//fmt.Printf("return old winners to availParticipants\n")
		//fmt.Printf("return winners: %v\n", oldWinners)
		//fmt.Printf("after return winners, availParticipants: %v\n", availParticipants)
	}*/

	need, err := needLottery(oldWinners, oldWinnerIndexes)
	if err != nil {
		return fmt.Errorf("needLottery() error: %v\n", err)
	}

	if !need {
		return fmt.Errorf("no need")
	}

	fmt.Printf("need = true\n")

	if len(prizes) <= 0 {
		return fmt.Errorf("no prizes")
	}

	if prizeIndex < 0 || prizeIndex >= len(prizes) {
		return fmt.Errorf("prize index error")
	}

	return nil
}

func getPrizeNum(prizes []Prize, prizeIndex int, oldWinnerIndexes []int) (int, error) {
	prizeNum := 0
	if len(oldWinnerIndexes) > 0 {
		prizeNum = len(oldWinnerIndexes)
	} else {
		prizeNum = prizes[prizeIndex].Num
	}

	if prizeNum <= 0 {
		return 0, fmt.Errorf("no prizes for prize index: %v\n", prizeIndex)
	}

	return prizeNum, nil
}
