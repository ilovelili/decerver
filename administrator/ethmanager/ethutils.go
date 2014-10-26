package ethmanager

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"github.com/eris-ltd/thelonious"
	"github.com/eris-ltd/thelonious/ethchain"
	"github.com/eris-ltd/thelonious/ethlog"
	"github.com/eris-ltd/thelonious/ethstate"
	"github.com/eris-ltd/thelonious/ethutil"
	"github.com/golang/glog"
	"io"
	"log"
	"math/big"
	"strings"
	"sync"
)

const (
	ERR_NO_SUCH_BLOCK        = "NO SUCH BLOCK"
	ERR_NO_SUCH_TX           = "NO SUCH TX"
	ERR_NO_SUCH_ADDRESS      = "NO SUCH ADDRESS"
	ERR_STATE_NO_STORAGE     = "STATE NO STORAGE"
	ERR_MALFORMED_ADDRESS    = "MALFORMED ADDRESS"
	ERR_MALFORMED_BLOCK_HASH = "MALFORMED BLOCK HASH"
	ERR_MALFORMED_TX_HASH    = "MALFORMED TX HASH"
)

const (
	ACCOUNT_MODIFIED = iota
	ACCOUNT_CREATED
	ACCOUNT_DELETED
)

type NoArgs struct {
}

type VString struct {
	SVal string
}

type VStringArr struct {
	Svals []string
}

type VBool struct {
	BVal bool
}

type VInteger struct {
	IVal int
}

type BlockMiniData struct {
	Number           string
	Hash             string
	Transactions     int
	PrevHash         string
	AccountsAffected []*AccountMini
}

type StateAtArgs struct {
	Address string
	Storage string
}

type TransactionArgs struct {
	BlockHash string
	TxHash    string
}

type BlockData struct {
	Number       string
	Time         int
	Nonce        string
	Hash         string
	PrevHash     string
	Difficulty   string
	Coinbase     string
	Transactions []*Transaction
	Uncles       []string
	GasLimit     string
	GasUsed      string
	MinGasPrice  string
	TxSha        string
	UncleSha     string
}

type Transaction struct {
	ContractCreation bool
	Nonce            string
	Hash             string
	Sender           string
	Recipient        string
	Value            string
	Gas              string
	GasCost          string
	BlockHash        string
	Error            string
}

type TransactionArr struct {
	Transactions []*Transaction
}

type Sync struct {
	NewBlocks []*BlockMiniData
	Ether     string
	IsMining  bool
	PreTxs    []string
	PostTxs   []string
}

type CompilerResult struct {
	Bytes   string
	Errors  []string
	Success bool
}

type TxIndata struct {
	Recipient string
	Value     string
	Gas       string
	GasCost   string
	Data      string
}

type TxReceipt struct {
	Success  bool   // If transaction hash was created basically.
	Compiled bool   // If a contract was created, and the txdata was successfully compiled.
	Address  string // If a contract was created.
	Hash     string // Transaction hash
	Error    string
}

type AccountMini struct {
	// Modified (0), Added (1), Deleted(2)
	Flag     int
	Contract bool
	Address  string
	Nonce    int
	Value    string
}

type Account struct {
	Contract bool
	Address  string
	Nonce    int
	Value    string
	Code     string
	Storage  []string
}

type Accounts struct {
	List []*Account
}

func getLastBlockNumber(ethereum *eth.Ethereum) int {
	return int(ethereum.BlockChain().LastBlockNumber)
}

func getWorldState(ethereum *eth.Ethereum) []*BlockMiniData {
	lastNum := ethereum.BlockChain().LastBlockNumber
	ctr := int(lastNum)
	fmt.Printf("Last Block Number: %d\n", lastNum)
	blocks := make([]*BlockMiniData, ctr+1)
	block := ethereum.BlockChain().CurrentBlock
	fmt.Printf("Current Block Number: %s\n", block.Number.String())
	bmd := &BlockMiniData{}
	getBlockMiniWSFromBlock(bmd, block)
	blocks[ctr] = bmd
	fmt.Printf("Current Block Mini: %v\n", bmd)
	ctr--
	for ctr >= 0 {
		pHash := block.PrevHash
		block = ethereum.BlockChain().GetBlock(pHash)
		fmt.Printf("Current Block Number: %s\n", block.Number.String())
		bmd := &BlockMiniData{}
		getBlockMiniWSFromBlock(bmd, block)
		blocks[ctr] = bmd
		fmt.Printf("Current Block Mini: %v\n", bmd)
		ctr--
	}

	return blocks
}

// Used during world state generation, when we don't care about the transactions.
func getBlockMiniWSFromBlock(reply *BlockMiniData, block *ethchain.Block) {

	reply.Number = block.Number.String()
	reply.Hash = hex.EncodeToString(block.Hash())

	if block.Transactions() != nil && len(block.Transactions()) > 0 {
		size := len(block.Transactions())
		reply.Transactions = size
	} else {
		reply.Transactions = 0
	}
}

// Used in block updates from reactor, when we want account diffs along with the block data.
func getBlockMiniDataFromBlock(ethereum *eth.Ethereum, reply *BlockMiniData, block *ethchain.Block) {

	reply.Number = block.Number.String()
	reply.Hash = hex.EncodeToString(block.Hash())

	aa := make(map[string]int)
	size := len(block.Transactions())
	reply.Transactions = size

	// Just check who sender and receiver is. Receiver may be a contract
	// creation address or a transaction receiver; either way it's a valid
	// account.
	for _, tx := range block.Transactions() {

		sender := hex.EncodeToString(tx.Sender())
		// Sender cannot be anything other then modified, which
		// does not change the flag. It can however be unset.
		if _, ok := aa[sender]; !ok {
			aa[sender] = ACCOUNT_MODIFIED
		}

		// This flag is used for the receiver (or creation address).
		rFlag := ACCOUNT_MODIFIED
		var rcBytes []byte
		if tx.CreatesContract() {
			rFlag |= ACCOUNT_CREATED
			rcBytes = tx.CreationAddress()
		} else {
			rcBytes = tx.Recipient
		}

		receiver := hex.EncodeToString(rcBytes)
		// Receiver
		if _, ok := aa[receiver]; !ok {
			aa[receiver] = rFlag
		} else {
			aa[receiver] |= rFlag
		}

	}

	// Coinbase
	cbAddr := hex.EncodeToString(block.Coinbase)

	if _, ok := aa[cbAddr]; !ok {
		aa[cbAddr] = ACCOUNT_MODIFIED
	}

	reply.AccountsAffected = make([]*AccountMini, len(aa))
	// For the final step, we check if all the affected contracts still exist. If any of
	// the contracts has been removed, we update the flag to DELETED.
	ctr := 0

	for addr, flag := range aa {
		// TODO really convert back and forth between bytes...
		addrBytes, _ := hex.DecodeString(addr)
		stObj := ethereum.BlockChain().CurrentBlock.State().GetStateObject(addrBytes)
		am := &AccountMini{}
		if stObj == nil {
			am.Address = addr
			am.Flag = ACCOUNT_DELETED
		} else {
			am.Address = addr
			am.Nonce = int(stObj.Nonce)
			am.Value = stObj.Balance.String()
			am.Flag = flag
		}
		reply.AccountsAffected[ctr] = am
		ctr++
	}

	// Block PrevHash
	if block.PrevHash != nil && bytes.Compare(block.PrevHash, ethchain.ZeroHash160) != 0 {
		reply.PrevHash = hex.EncodeToString(block.PrevHash)
	}
}

func getBlockDataFromBlock(reply *BlockData, block *ethchain.Block) {

	// Block Number
	reply.Number = block.Number.String()

	// Block Time
	reply.Time = int(block.Time)

	// Block Nonce
	reply.Nonce = hex.EncodeToString(block.Nonce)

	// Block Transactions (hashes)
	trsct := block.Transactions()

	reply.Transactions = make([]*Transaction, len(trsct))
	if trsct != nil {
		for idx := range trsct {
			reply.Transactions[idx] = &Transaction{}
			getTransactionFromTx(reply.Transactions[idx], trsct[idx])
		}
	}

	// Block Hash
	reply.Hash = hex.EncodeToString(block.Hash())

	// Block PrevHash
	if block.PrevHash != nil && bytes.Compare(block.PrevHash, ethchain.ZeroHash256) != 0 {
		reply.PrevHash = hex.EncodeToString(block.PrevHash)
	}

	// Block Difficulty
	reply.Difficulty = block.Difficulty.String()

	// Block Coinbase
	reply.Coinbase = hex.EncodeToString(block.Coinbase)

	// Block Uncles (hashes)
	uncles := block.Uncles

	reply.Uncles = make([]string, len(uncles))
	if uncles != nil {
		for idx := range uncles {
			reply.Uncles[idx] = hex.EncodeToString(uncles[idx].Hash())
		}
	}

	reply.GasLimit = block.GasLimit.String()
	reply.GasUsed = block.GasUsed.String()

	reply.MinGasPrice = block.MinGasPrice.String()

	reply.TxSha = hex.EncodeToString(block.TxSha)
	reply.UncleSha = hex.EncodeToString(block.UncleSha)
}

func createTx(eth *eth.Ethereum, recipient, valueStr, gasStr, gasPriceStr, scriptStr string, reply *TxReceipt) error {
	var contractCreation bool
	if len(recipient) == 0 {
		contractCreation = true
	}
	hash, _ := hex.DecodeString(recipient)
	fmt.Printf("Recipient: %x\n", hash)
	value := ethutil.Big(valueStr)
	gas := ethutil.Big(gasStr)
	gasPrice := ethutil.Big(gasPriceStr)
	var tx *ethchain.Transaction
	// Compile and assemble the given data
	if contractCreation {
		var script []byte
		var err error
		if ethutil.IsHex(scriptStr) {
			script, err = hex.DecodeString(scriptStr)
			reply.Compiled = false
		} else {
			script, err = ethutil.Compile(scriptStr, false)
			reply.Compiled = true
		}
		if err != nil {

			return err
		}

		tx = ethchain.NewContractCreationTx(value, gas, gasPrice, script)
	} else {
		data := ethutil.StringToByteFunc(scriptStr, func(s string) (ret []byte) {
			slice := strings.Split(s, "\n")
			for _, dataItem := range slice {
				d := ethutil.FormatData(dataItem)
				ret = append(ret, d...)
			}
			return
		})

		tx = ethchain.NewTransactionMessage(hash, value, gas, gasPrice, data)
	}
	keyPair := eth.KeyManager().KeyPair()
	acc := eth.StateManager().TransState().GetOrNewStateObject(keyPair.Address())
	tx.Nonce = acc.Nonce
	acc.Nonce += 1
	eth.StateManager().TransState().UpdateStateObject(acc)

	tx.Sign(keyPair.PrivateKey)
	eth.TxPool().QueueTransaction(tx)

	// Now write
	if contractCreation {
		reply.Address = hex.EncodeToString(tx.CreationAddress())
		glog.Infof("Contract addr %x", tx.CreationAddress())
	}

	reply.Hash = hex.EncodeToString(tx.Hash())
	reply.Success = true

	return nil
}

func getStateObject(ethereum *eth.Ethereum, address string) *ethstate.StateObject {
	stateObject := ethereum.StateManager().CurrentState().GetStateObject(ethutil.Hex2Bytes(address))
	if stateObject != nil {
		return stateObject
	}
	return nil
}

func stateExists(ethereum *eth.Ethereum, address string) bool {
	sObj := getStateObject(ethereum, address)
	if sObj == nil {
		return false
	}
	return true
}

func isContract(ethereum *eth.Ethereum, address string) bool {
	sObj := getStateObject(ethereum, address)
	if sObj != nil && len(sObj.Code) > 0 {
		return true
	}
	return false
}

func getTransactionFromTx(trans *Transaction, tx *ethchain.Transaction) {

	trans.ContractCreation = tx.CreatesContract()
	if trans.ContractCreation {
		trans.Recipient = hex.EncodeToString(tx.CreationAddress())
	} else {
		trans.Recipient = hex.EncodeToString(tx.Recipient)
	}
	trans.Sender = hex.EncodeToString(tx.Sender())
	trans.Gas = tx.Gas.String()
	trans.GasCost = tx.GasPrice.String()
	trans.Nonce = big.NewInt(int64(tx.Nonce)).String()
	trans.Hash = hex.EncodeToString(tx.Hash())
}

func getAccounts(ethereum *eth.Ethereum) []*AccountMini {
	accounts := []*AccountMini{}
	block := ethereum.BlockChain().CurrentBlock
	state := block.State()
	it := state.Trie.NewIterator()
	it.Each(func(key string, value *ethutil.Value) {
		addr := ethutil.Address([]byte(key))
		// obj := ethstate.NewStateObjectFromBytes(addr, value.Bytes())
		obj := block.State().GetAccount(addr)
		acc := &AccountMini{}
		acc.Address = ethutil.Bytes2Hex([]byte(addr))
		acc.Nonce = int(obj.Nonce)
		acc.Value = obj.Balance.String()
		acc.Contract = obj.Code != nil || obj.InitCode != nil
		accounts = append(accounts, acc)
	})
	return accounts
}

func getAccountMiniFromStateObject(account *AccountMini, st *ethstate.StateObject) {

	account.Address = hex.EncodeToString(st.Address())
	account.Contract = len(st.Code) > 0 || len(st.InitCode) > 0
	account.Value = st.Balance.String()
	account.Nonce = int(st.Nonce)

	return
}

func getAccountFromStateObject(account *Account, st *ethstate.StateObject) {

	account.Address = hex.EncodeToString(st.Address())
	account.Contract = len(st.Code) > 0 || len(st.InitCode) > 0
	account.Value = st.Balance.String()
	account.Nonce = int(st.Nonce)

	if len(st.Code) > 0 {
		account.Code = hex.EncodeToString(st.Code)
	}

	storage := []string{}
	st.EachStorage(func(key string, node *ethutil.Value) {
		bytes := []byte(key)
		storage = append(storage, hex.EncodeToString(bytes))
		storage = append(storage, hex.EncodeToString(RLPDecode(node.Bytes())))
	})
	account.Storage = storage
	return
}

// TODO while testing
type LogSub struct {
	Channel  chan string
	SubId    uint32
	LogLevel ethlog.LogLevel
	Enabled  bool
}

func NewStdLogSub() *LogSub {
	ls := &LogSub{
		Channel:  make(chan string),
		SubId:    0,
		LogLevel: ethlog.LogLevel(5),
		Enabled:  true,
	}
	return ls
}

type EthLogger struct {
	mutex     *sync.Mutex
	logReader io.Reader
	logWriter io.Writer
	logLevel  ethlog.LogLevel
	subs      []*LogSub
}

func NewEthLogger() *EthLogger {
	el := &EthLogger{}
	el.mutex = &sync.Mutex{};
	el.logLevel = ethlog.LogLevel(5)
	el.logReader, el.logWriter = io.Pipe()

	ethlog.AddLogSystem(ethlog.NewStdLogSystem(el.logWriter, log.LstdFlags, el.logLevel))

	go func(el *EthLogger) {
		scanner := bufio.NewScanner(el.logReader)
		for scanner.Scan() {
			text := scanner.Text()
			el.mutex.Lock()
			for _, sub := range el.subs {
				sub.Channel <- text
			}
			el.mutex.Unlock()
		}
	}(el)
	return el
}

func (el *EthLogger) AddSub(sub *LogSub) {
	el.mutex.Lock()
	el.subs = append(el.subs, sub)
	el.mutex.Unlock()
}

func (el *EthLogger) RemoveSub(sub *LogSub) {
	el.mutex.Lock()
	theIdx := -1
	for idx, s := range el.subs {
		if sub.SubId == s.SubId {
			theIdx = idx
			break
		}
	}
	if theIdx >= 0 {
		el.subs = append(el.subs[:theIdx], el.subs[theIdx+1:]...)
	}
	el.mutex.Unlock()
}

func RLPDecode(data []byte) []byte {
	
	char := int(data[0])
	switch {
	case char <= 0x7f:
		return data
	case char <= 0xb7:
		b := uint64(data[0]) - 0x80
		return data[1 : 1+b]
	default:
		panic(fmt.Sprintf("byte not supported: %q", char))
	}

	return nil
}
