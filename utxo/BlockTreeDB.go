package utxo

import (
	"github.com/btcboost/copernicus/orm/database"
	"github.com/btcboost/copernicus/conf"
	"path/filepath"
	"github.com/btcboost/copernicus/orm"
)

type BlockTreeDB struct {
	database.DBBase
	bucketKey string
}

func NewBlockTreeDB() *BlockTreeDB {
	blockTreeDB := new(BlockTreeDB)
	path := conf.AppConf.DataDir + string(filepath.Separator) + "blocks" + string(filepath.Separator) + "index"
	db, err := orm.InitDB(orm.DBBolt, path)
	if err != nil {
		panic(err)
	}
	_, err = db.CreateIfNotExists([]byte("index"))
	if err != nil {
		panic(err)
	}
	blockTreeDB.DBBase = db
	blockTreeDB.bucketKey = "index"
	return blockTreeDB
}
