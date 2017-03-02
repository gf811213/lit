package wallit

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/boltdb/bolt"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil/hdkeychain"
	"github.com/mit-dci/lit/lnutil"
	"github.com/mit-dci/lit/uspv"
)

func NewWallit(
	rootkey *hdkeychain.ExtendedKey,
	height int32, spvhost, path string, p *chaincfg.Params) *Wallit {

	var w Wallit
	w.rootPrivKey = rootkey
	w.Param = p
	w.FreezeSet = make(map[wire.OutPoint]*FrozenTx)

	wallitpath := filepath.Join(path, p.Name)

	// create wallit sub dir if it's not there
	_, err := os.Stat(wallitpath)
	if os.IsNotExist(err) {
		os.Mkdir(wallitpath, 0700)
	}

	u := new(uspv.SPVCon)
	w.Hook = u

	incomingTx, incomingBlockheight, err := w.Hook.Start(height, spvhost, wallitpath, p)
	if err != nil {
		fmt.Printf("crash  %s ", err.Error())
	}

	wallitdbname := filepath.Join(wallitpath, "utxo.db")
	err = w.OpenDB(wallitdbname)
	if err != nil {
		fmt.Printf("crash  %s ", err.Error())
	}

	// deal with the incoming txs

	go w.TxHandler(incomingTx)

	// deal with incoming height

	go w.HeightHandler(incomingBlockheight)

	return &w
}

func (w *Wallit) TxHandler(incomingTxAndHeight chan lnutil.TxAndHeight) {
	for {
		txah := <-incomingTxAndHeight
		w.Ingest(txah.Tx, txah.Height)
		fmt.Printf("got tx %s at height %d\n",
			txah.Tx.TxHash().String(), txah.Height)
	}
}

func (w *Wallit) HeightHandler(incomingHeight chan int32) {
	for {
		h := <-incomingHeight
		fmt.Printf("got height %d\n", h)
	}
}

// OpenDB starts up the database.  Creates the file if it doesn't exist.
func (w *Wallit) OpenDB(filename string) error {
	var err error
	var numKeys uint32
	w.StateDB, err = bolt.Open(filename, 0644, nil)
	if err != nil {
		return err
	}
	// create buckets if they're not already there
	err = w.StateDB.Update(func(btx *bolt.Tx) error {
		_, err = btx.CreateBucketIfNotExists(BKToutpoint)
		if err != nil {
			return err
		}
		_, err = btx.CreateBucketIfNotExists(BKTadr)
		if err != nil {
			return err
		}
		_, err = btx.CreateBucketIfNotExists(BKTStxos)
		if err != nil {
			return err
		}
		_, err = btx.CreateBucketIfNotExists(BKTTxns)
		if err != nil {
			return err
		}

		sta, err := btx.CreateBucketIfNotExists(BKTState)
		if err != nil {
			return err
		}

		numKeysBytes := sta.Get(KEYNumKeys)
		if numKeysBytes != nil { // NumKeys exists, read into uint32
			numKeys = lnutil.BtU32(numKeysBytes)
			fmt.Printf("db says %d keys\n", numKeys)
		} else { // no adrs yet, make it 0.  Then make an address.
			fmt.Printf("NumKeys not in DB, must be new DB. 0 Keys\n")
			numKeys = 0
			b0 := lnutil.U32tB(numKeys)
			err = sta.Put(KEYNumKeys, b0)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return err
	}

	// make a new address if the DB is new.  Might not work right with no addresses.
	if numKeys == 0 {
		_, err := w.NewAdr160()
		if err != nil {
			return err
		}
	}
	return nil
}