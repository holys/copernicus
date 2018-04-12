package mempool

import (
	"fmt"
	"math"
	"sync"

	"container/list"
	"github.com/astaxie/beego/logs"
	"github.com/btcboost/copernicus/core"
	"github.com/btcboost/copernicus/utils"
	"github.com/btcboost/copernicus/utxo"
	"github.com/google/btree"
	"unsafe"
)

type PoolRemovalReason int

// Reason why a transaction was removed from the memPool, this is passed to the
// * notification signal.
const (
	// UNKNOWN Manually removed or unknown reason
	UNKNOWN PoolRemovalReason = iota
	// EXPIRY Expired from memPool
	EXPIRY
	// SIZELIMIT Removed in size limiting
	SIZELIMIT
	// REORG Removed for reorganization
	REORG
	// BLOCK Removed for block
	BLOCK
	// CONFLICT Removed for conflict with in-block transaction
	CONFLICT
	// REPLACED Removed for replacement
	REPLACED
)

// TxMempool is safe for concurrent write And read access.
type TxMempool struct {
	sync.RWMutex
	// current mempool best feerate for one transaction.
	fee utils.FeeRate
	// poolData store the tx in the mempool
	PoolData map[utils.Hash]*TxEntry
	//NextTx key is txPreout, value is tx.
	NextTx map[core.OutPoint]*TxEntry
	//RootTx all tx's ancestor transaction number is 1.
	RootTx          map[utils.Hash]*TxEntry
	timeSortData    *btree.BTree
	cacheInnerUsage int64
	checkFrequency  float64
	// sum of all mempool tx's size.
	totalTxSize uint64
	//transactionsUpdated mempool update transaction total number when create mempool late.
	transactionsUpdated uint64
}

func (m *TxMempool) GetCheckFreQuency() float64 {
	m.RLock()
	defer m.RUnlock()
	return m.checkFrequency
}

// Check If sanity-checking is turned on, check makes sure the pool is consistent
// (does not contain two transactions that spend the same inputs, all inputs
// are in the mapNextTx array). If sanity-checking is turned off, check does
// nothing.
func (m *TxMempool) Check(coins *utxo.CoinsViewCache, bestHeight int) {
	if m.GetCheckFreQuency() == 0 {
		return
	}
	if float64(utils.GetRand(math.MaxUint32)) >= m.GetCheckFreQuency() {
		return
	}

	checkTotal := uint64(0)
	innerUsage := uint64(0)
	m.Lock()
	defer m.Unlock()

	waitingOnDependants := list.New()
	for _, entry := range m.PoolData {
		//i := uint(0)
		checkTotal += uint64(entry.GetTxSize())
		innerUsage += uint64(entry.usageSize)
		fDependsWait := false
		setParentCheck := make(map[utils.Hash]struct{})

		for _, txin := range entry.tx.Ins {
			if entry, ok := m.PoolData[txin.PreviousOutPoint.Hash]; ok {
				tx2 := entry.tx
				if !(len(tx2.Outs) > int(txin.PreviousOutPoint.Index) &&
					!tx2.Outs[txin.PreviousOutPoint.Index].IsNull()) {
					panic("the tx introduced input dose not exist, or the input amount is nil ")
				}
				fDependsWait = true
				setParentCheck[tx2.Hash] = struct{}{}
			} else {
				if coins.HaveCoin(txin.PreviousOutPoint) {
					panic("the tx introduced input dose not exist mempool And UTXO set !!!")
				}
			}

			if _, ok := m.NextTx[*txin.PreviousOutPoint]; !ok {
				panic("the introduced tx is not in mempool")
			}
		}
		if len(setParentCheck) != len(entry.parentTx) {
			panic("the two parent set should be equal")
		}

		// Verify ancestor state is correct.
		nNoLimit := uint64(math.MaxUint64)
		setAncestors, err := m.CalculateMemPoolAncestors(entry.tx, nNoLimit, nNoLimit, nNoLimit, nNoLimit, true)
		if err != nil {
			return
		}
		nCountCheck := int64(len(setAncestors)) + 1
		nSizeCheck := int64(entry.txSize)
		nSigOpCheck := int64(entry.sigOpCount)
		for ancestorIt := range setAncestors {
			nSizeCheck += int64(ancestorIt.txSize)
			nSigOpCheck += int64(ancestorIt.sigOpCount)
		}
		if entry.sumTxCountWithAncestors != nCountCheck {
			panic("the txentry's ancestors number is incorrect .")
		}
		if entry.sumSizeWitAncestors != nSizeCheck {
			panic("the txentry's ancestors size is incorrect .")
		}
		if entry.sumSigOpCountWithAncestors != nSigOpCheck {
			panic("the txentry's ancestors sigopcount is incorrect .")
		}

		// Also check to make sure size is greater than sum with immediate
		// children. Just a sanity check, not definitive that this calc is
		// correct...
		if fDependsWait {
			waitingOnDependants.PushBack(entry)
		} else {
			var state core.ValidationState
			fCheckResult := entry.tx.IsCoinBase() ||
				coins.CheckTxInputs(entry.tx, &state, bestHeight)
			if !fCheckResult {
				panic("the txentry check failed with utxo set...")
			}
			coins.UpdateCoins(entry.tx, 1000000)
		}
	}
	stepsSinceLastRemove := 0
	for waitingOnDependants.Len() > 0 {
		it := waitingOnDependants.Front()
		entry := it.Value.(*TxEntry)
		waitingOnDependants.Remove(it)
		if !coins.HaveInputs(entry.tx) {
			waitingOnDependants.PushBack(entry)
			stepsSinceLastRemove++
			if !(stepsSinceLastRemove < waitingOnDependants.Len()) {
				panic("")
			}
		} else {
			fCheckResult := entry.tx.IsCoinBase() ||
				coins.CheckTxInputs(entry.tx, nil, bestHeight)
			if !fCheckResult {
				panic("")
			}
			coins.UpdateCoins(entry.tx, 1000000)
			stepsSinceLastRemove = 0
		}
	}

	for _, entry := range m.NextTx {
		txid := entry.tx.Hash
		if e, ok := m.PoolData[txid]; !ok {
			panic("the transaction not exsit mempool. . .")
		} else {
			if e.tx != entry.tx {
				panic("mempool store the transaction is different with it's two struct . . .")
			}
		}
	}
}

// RemoveForBlock when a new valid block is received, so all the transaction
// in the block should removed from memPool.
func (m *TxMempool) RemoveForBlock(txs []*core.Tx, txHeight int) {
	m.Lock()
	defer m.Unlock()

	entries := make([]*TxEntry, 0, len(txs))
	for _, tx := range txs {
		if entry, ok := m.PoolData[tx.Hash]; ok {
			entries = append(entries, entry)
		}
	}

	// todo base on entries to set the new feerate for mempool.

	for _, tx := range txs {
		if entry, ok := m.PoolData[tx.Hash]; ok {
			stage := make(map[*TxEntry]struct{})
			stage[entry] = struct{}{}
			m.RemoveStaged(stage, true, BLOCK)
		}
		m.removeConflicts(tx)
	}
}

// AddTx operator is safe for concurrent write And read access.
// this function is used to add tx to the memPool, and now the tx should
// be passed all appropriate checks.
func (m *TxMempool) AddTx(txentry *TxEntry, limitAncestorCount uint64,
	limitAncestorSize uint64, limitDescendantCount uint64, limitDescendantSize uint64) error {
	// todo: send signal to all interesting the caller.
	m.Lock()
	defer m.Unlock()
	ancestors, err := m.CalculateMemPoolAncestors(txentry.tx, limitAncestorCount, limitAncestorSize, limitDescendantCount, limitDescendantSize, true)
	if err != nil {
		return err
	}

	// insert new txEntry to the memPool; and update the memPool's memory consume.
	m.PoolData[txentry.tx.Hash] = txentry
	m.timeSortData.ReplaceOrInsert(txentry)
	m.cacheInnerUsage += int64(txentry.usageSize) + int64(unsafe.Sizeof(txentry))

	// Update ancestors with information about this tx
	setParentTransactions := make(map[utils.Hash]struct{})
	tx := txentry.tx
	for _, txin := range tx.Ins {
		m.NextTx[*txin.PreviousOutPoint] = txentry
		setParentTransactions[txin.PreviousOutPoint.Hash] = struct{}{}
	}

	for hash := range setParentTransactions {
		if parent, ok := m.PoolData[hash]; ok {
			txentry.UpdateParent(parent, &m.cacheInnerUsage, true)
		}
	}

	m.updateAncestorsOf(true, txentry, ancestors)
	m.UpdateEntryForAncestors(txentry, ancestors)
	m.totalTxSize += uint64(txentry.txSize)
	m.transactionsUpdated++
	if txentry.sumTxCountWithAncestors == 1 {
		m.RootTx[txentry.tx.Hash] = txentry
		m.cacheInnerUsage += int64(unsafe.Sizeof(txentry.tx.Hash) + unsafe.Sizeof(txentry))
	}

	return nil
}

func (m *TxMempool) TrimToSize(sizeLimit int64, noSpendsRemaining *[]*core.OutPoint) {
	m.Lock()
	defer m.Unlock()

	nTxnRemoved := 0
	for _, remove := range m.PoolData {
		if m.cacheInnerUsage > sizeLimit {
			stage := make(map[*TxEntry]struct{})
			m.CalculateDescendants(remove, stage)
			nTxnRemoved += len(stage)

			txn := make([]*core.Tx, 0, len(stage))
			if noSpendsRemaining != nil {
				for iter := range stage {
					txn = append(txn, iter.tx)
				}
			}

			m.RemoveStaged(stage, false, SIZELIMIT)
			if noSpendsRemaining != nil {
				for _, tx := range txn {
					for _, txin := range tx.Ins {
						if m.FindTx(txin.PreviousOutPoint.Hash) != nil {
							continue
						}
						if _, ok := m.NextTx[*txin.PreviousOutPoint]; !ok {
							*noSpendsRemaining = append(*noSpendsRemaining, txin.PreviousOutPoint)
						}
					}
				}
			}
		}
	}
	logs.Debug("mempool remove %d transactions with SIZELIMIT reason. \n", nTxnRemoved)
}

// Expire all transaction (and their dependencies) in the memPool older
// than time. Return the number of removed transactions.
func (m *TxMempool) Expire(time int64) int {
	m.Lock()
	defer m.Unlock()
	toremove := make(map[*TxEntry]struct{}, 100)
	m.timeSortData.Ascend(func(i btree.Item) bool {
		entry := i.(*TxEntry)
		if entry.time < time {
			toremove[entry] = struct{}{}
			return true
		}
		return false
	})

	stage := make(map[*TxEntry]struct{}, len(toremove)*3)
	for removeIt := range toremove {
		m.CalculateDescendants(removeIt, stage)
	}
	m.RemoveStaged(stage, false, EXPIRY)
	return len(stage)
}

func (m *TxMempool) FindTx(hash utils.Hash) *core.Tx {
	m.RLock()
	m.RUnlock()
	if find, ok := m.PoolData[hash]; ok {
		return find.tx
	}
	return nil
}

// HasNoInputsOf Check that none of this transactions inputs are in the memPool,
// and thus the tx is not dependent on other memPool transactions to be included
// in a block.
func (m *TxMempool) HasNoInputsOf(tx *core.Tx) bool {
	m.RLock()
	defer m.RUnlock()

	for _, txin := range tx.Ins {
		if m.FindTx(txin.PreviousOutPoint.Hash) != nil {
			return false
		}
	}
	return true
}

func (m *TxMempool) updateForRemoveFromMempool(entriesToRemove map[*TxEntry]struct{}, updateDescendants bool) {
	nNoLimit := uint64(math.MaxUint64)

	if updateDescendants {
		for removeIt := range entriesToRemove {
			setDescendants := make(map[*TxEntry]struct{})
			m.CalculateDescendants(removeIt, setDescendants)
			delete(setDescendants, removeIt)
			modifySize := -removeIt.txSize
			modifyFee := -removeIt.txFee
			modifySigOps := -removeIt.sigOpCount

			for dit := range setDescendants {
				dit.UpdateAncestorState(-1, modifySize, modifySigOps, modifyFee)
				if _, ok := m.RootTx[removeIt.tx.Hash]; ok {
					if dit.sumTxCountWithAncestors == 1 {
						m.RootTx[dit.tx.Hash] = dit
					}
				}
			}
		}
	}

	for removeIt := range entriesToRemove {
		ancestors, err := m.CalculateMemPoolAncestors(removeIt.tx, nNoLimit, nNoLimit, nNoLimit, nNoLimit, false)
		if err != nil {
			return
		}
		m.updateAncestorsOf(false, removeIt, ancestors)
	}

	for removeIt := range entriesToRemove {
		if _, ok := m.RootTx[removeIt.tx.Hash]; ok {
			delete(m.RootTx, removeIt.tx.Hash)
			m.cacheInnerUsage -= int64(unsafe.Sizeof(removeIt.tx.Hash) + unsafe.Sizeof(removeIt))
		}
		for updateIt := range removeIt.childTx {
			updateIt.UpdateParent(removeIt, &m.cacheInnerUsage, false)
		}
	}
}

func (m *TxMempool) RemoveStaged(entriesToRemove map[*TxEntry]struct{}, updateDescendants bool, reason PoolRemovalReason) {

	m.updateForRemoveFromMempool(entriesToRemove, updateDescendants)
	for rem := range entriesToRemove {
		m.delTxentry(rem, reason)
	}
}

func (m *TxMempool) removeConflicts(tx *core.Tx) {
	// Remove transactions which depend on inputs of tx, recursively
	for _, txin := range tx.Ins {
		if flictEntry, ok := m.NextTx[*txin.PreviousOutPoint]; ok {
			if flictEntry.tx.Hash != tx.Hash {
				m.removeRecursive(flictEntry.tx, CONFLICT)
			}
		}
	}
}

func (m *TxMempool) removeRecursive(origTx *core.Tx, reason PoolRemovalReason) {
	// Remove transaction from memory pool
	txToRemove := make(map[*TxEntry]struct{})

	if entry, ok := m.PoolData[origTx.Hash]; ok {
		txToRemove[entry] = struct{}{}
	} else {
		// When recursively removing but origTx isn't in the mempool be sure
		// to remove any children that are in the pool. This can happen
		// during chain re-orgs if origTx isn't re-accepted into the mempool
		// for any reason.
		for i := range origTx.Outs {
			outPoint := core.OutPoint{Hash: origTx.Hash, Index: uint32(i)}
			if en, ok := m.NextTx[outPoint]; !ok {
				continue
			} else {
				if find, ok := m.PoolData[en.tx.Hash]; ok {
					txToRemove[find] = struct{}{}
				} else {
					panic("the transaction must in mempool, because NextTx struct of mempool have its data")
				}
			}
		}
	}
	allRemoves := make(map[*TxEntry]struct{})
	for it := range txToRemove {
		m.CalculateDescendants(it, allRemoves)
	}
	m.RemoveStaged(allRemoves, false, reason)
}

// CalculateDescendants Calculates descendants of entry that are not already in setDescendants, and
// adds to setDescendants. Assumes entry it is already a tx in the mempool and
// setMemPoolChildren is correct for tx and all descendants. Also assumes that
// if an entry is in setDescendants already, then all in-mempool descendants of
// it are already in setDescendants as well, so that we can save time by not
// iterating over those entries.
func (m *TxMempool) CalculateDescendants(entry *TxEntry, descendants map[*TxEntry]struct{}) {
	stage := make(map[*TxEntry]struct{})
	if _, ok := descendants[entry]; !ok {
		stage[entry] = struct{}{}
	}

	// Traverse down the children of entry, only adding children that are not
	// accounted for in setDescendants already (because those children have
	// either already been walked, or will be walked in this iteration).
	for desEntry := range stage {
		descendants[desEntry] = struct{}{}
		delete(stage, desEntry)

		for child := range desEntry.childTx {
			if _, ok := descendants[child]; !ok {
				stage[child] = struct{}{}
			}
		}
	}

}

// updateAncestorsOf update each of ancestors transaction state; add or remove this
// txentry txfee, txsize, txcount.
func (m *TxMempool) updateAncestorsOf(add bool, txentry *TxEntry, ancestors map[*TxEntry]struct{}) {

	// update the parent's child transaction set;
	for piter := range txentry.parentTx {
		if add {
			piter.UpdateChild(txentry, &m.cacheInnerUsage, true)
		} else {
			piter.UpdateChild(txentry, &m.cacheInnerUsage, false)
		}
	}

	updateCount := -1
	if add {
		updateCount = 1
	}
	updateSize := updateCount * txentry.txSize
	updateFee := int64(updateCount) * txentry.txFee
	// update each of ancestors transaction state;
	for ancestorit := range ancestors {
		ancestorit.UpdateDescendantState(updateCount, updateSize, updateFee)
	}
}

func (m *TxMempool) UpdateEntryForAncestors(entry *TxEntry, setAncestors map[*TxEntry]struct{}) {
	updateCount := len(setAncestors)
	updateSize := 0
	updateFee := int64(0)
	updateSigOpsCount := 0

	for ancestorIt := range setAncestors {
		updateFee += ancestorIt.txFee
		updateSigOpsCount += ancestorIt.sigOpCount
		updateSize += ancestorIt.txSize
	}
	entry.UpdateAncestorState(updateCount, updateSize, updateSigOpsCount, updateFee)
}

// CalculateMemPoolAncestors get tx all ancestors transaction in mempool.
// when the find is false: the tx must in mempool, so directly get his parent.
func (m *TxMempool) CalculateMemPoolAncestors(tx *core.Tx, limitAncestorCount uint64,
	limitAncestorSize uint64, limitDescendantCount uint64, limitDescendantSize uint64,
	searchForParent bool) (ancestors map[*TxEntry]struct{}, err error) {

	ancestors = make(map[*TxEntry]struct{})
	parent := make(map[*TxEntry]struct{})
	if searchForParent {
		for _, txin := range tx.Ins {
			if entry, ok := m.PoolData[txin.PreviousOutPoint.Hash]; ok {
				parent[entry] = struct{}{}
				if uint64(len(parent))+1 > limitAncestorCount {
					return nil,
						fmt.Errorf("too many unconfirmed parents [limit: %d]", limitAncestorCount)
				}
			}
		}
	} else {
		// If we're not searching for parents, we require this to be an entry in
		// the mempool already.
		if entry, ok := m.PoolData[tx.Hash]; ok {
			parent = entry.parentTx
		} else {
			panic("the tx must be in mempool")
		}
	}

	totalSizeWithAncestors := int64(tx.SerializeSize())

	for entry := range parent {
		delete(parent, entry)
		ancestors[entry] = struct{}{}
		totalSizeWithAncestors += int64(entry.txSize)
		hash := entry.tx.Hash
		if uint64(entry.sumSizeWithDescendants+int64(entry.txSize)) > limitDescendantSize {
			return nil,
				fmt.Errorf("exceeds descendant size limit for tx %s [limit: %d]", hash.ToString(), limitDescendantSize)
		} else if uint64(entry.sumTxCountWithDescendants+1) > limitDescendantCount {
			return nil,
				fmt.Errorf("too many descendants for tx %s [limit: %d]", hash.ToString(), limitDescendantCount)
		} else if uint64(totalSizeWithAncestors) > limitAncestorSize {
			return nil,
				fmt.Errorf("exceeds ancestor size limit [limit: %d]", limitAncestorSize)
		}

		graTxentrys := entry.parentTx
		for gentry := range graTxentrys {
			if _, ok := ancestors[gentry]; !ok {
				parent[gentry] = struct{}{}
			}
			if uint64(len(parent)+len(ancestors)+1) > limitAncestorCount {
				return nil,
					fmt.Errorf("too many unconfirmed ancestors [limit: %d]", limitAncestorCount)
			}
		}
	}

	return ancestors, nil
}

func (m *TxMempool) delTxentry(removeEntry *TxEntry, reason PoolRemovalReason) {
	// todo add signal for any subscriber

	for _, txin := range removeEntry.tx.Ins {
		delete(m.NextTx, *txin.PreviousOutPoint)
	}

	m.cacheInnerUsage -= int64(removeEntry.usageSize) + int64(unsafe.Sizeof(removeEntry))
	m.transactionsUpdated++
	m.totalTxSize -= uint64(removeEntry.txSize)
	delete(m.PoolData, removeEntry.tx.Hash)
	m.timeSortData.Delete(removeEntry)
}

func NewTxMempool() *TxMempool {
	t := &TxMempool{}
	t.NextTx = make(map[core.OutPoint]*TxEntry)
	t.PoolData = make(map[utils.Hash]*TxEntry)
	t.timeSortData = btree.New(32)
	return t
}
