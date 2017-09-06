package model

import (
	"testing"

	"fmt"

	"bytes"

	"github.com/btcboost/copernicus/core"
	"github.com/btcboost/copernicus/utils"
)

var testsTx = []struct {
	hash string //transaction  hash
	tx   Tx     //transaction  obj
}{
	{
		hash: "fff2525b8931402dd09222c50775608f75787bd2b87e56995a7bdd30f79702c4",
		tx: Tx{
			Version: 1,
			Ins: []*TxIn{
				{
					PreviousOutPoint: &OutPoint{
						Hash: &utils.Hash{
							0x03, 0x2e, 0x38, 0xe9, 0xc0, 0xa8, 0x4c, 0x60,
							0x46, 0xd6, 0x87, 0xd1, 0x05, 0x56, 0xdc, 0xac,
							0xc4, 0x1d, 0x27, 0x5e, 0xc5, 0x5f, 0xc0, 0x07,
							0x79, 0xac, 0x88, 0xfd, 0xf3, 0x57, 0xa1, 0x87,
						},
						Index: 0,
					},
					Script: &Script{
						bytes: []byte{
							0x49, // OP_DATA_73
							0x30, 0x46, 0x02, 0x21, 0x00, 0xc3, 0x52, 0xd3,
							0xdd, 0x99, 0x3a, 0x98, 0x1b, 0xeb, 0xa4, 0xa6,
							0x3a, 0xd1, 0x5c, 0x20, 0x92, 0x75, 0xca, 0x94,
							0x70, 0xab, 0xfc, 0xd5, 0x7d, 0xa9, 0x3b, 0x58,
							0xe4, 0xeb, 0x5d, 0xce, 0x82, 0x02, 0x21, 0x00,
							0x84, 0x07, 0x92, 0xbc, 0x1f, 0x45, 0x60, 0x62,
							0x81, 0x9f, 0x15, 0xd3, 0x3e, 0xe7, 0x05, 0x5c,
							0xf7, 0xb5, 0xee, 0x1a, 0xf1, 0xeb, 0xcc, 0x60,
							0x28, 0xd9, 0xcd, 0xb1, 0xc3, 0xaf, 0x77, 0x48,
							0x01, // 73-byte signature
							0x41, // OP_DATA_65
							0x04, 0xf4, 0x6d, 0xb5, 0xe9, 0xd6, 0x1a, 0x9d,
							0xc2, 0x7b, 0x8d, 0x64, 0xad, 0x23, 0xe7, 0x38,
							0x3a, 0x4e, 0x6c, 0xa1, 0x64, 0x59, 0x3c, 0x25,
							0x27, 0xc0, 0x38, 0xc0, 0x85, 0x7e, 0xb6, 0x7e,
							0xe8, 0xe8, 0x25, 0xdc, 0xa6, 0x50, 0x46, 0xb8,
							0x2c, 0x93, 0x31, 0x58, 0x6c, 0x82, 0xe0, 0xfd,
							0x1f, 0x63, 0x3f, 0x25, 0xf8, 0x7c, 0x16, 0x1b,
							0xc6, 0xf8, 0xa6, 0x30, 0x12, 0x1d, 0xf2, 0xb3,
							0xd3, // 65-byte pubkey
						},
					},
					Sequence: 0xffffffff,
				},
			},
			Outs: []*TxOut{
				{
					Value: 0x2123e300, // 556000000
					Script: &Script{
						bytes: []byte{
							0x76, // OP_DUP
							0xa9, // OP_HASH160
							0x14, // OP_DATA_20
							0xc3, 0x98, 0xef, 0xa9, 0xc3, 0x92, 0xba, 0x60,
							0x13, 0xc5, 0xe0, 0x4e, 0xe7, 0x29, 0x75, 0x5e,
							0xf7, 0xf5, 0x8b, 0x32,
							0x88, // OP_EQUALVERIFY
							0xac, // OP_CHECKSIG
						},
					},
				},
				{
					Value: 0x108e20f00, // 4444000000
					Script: &Script{
						bytes: []byte{
							0x76, // OP_DUP
							0xa9, // OP_HASH160
							0x14, // OP_DATA_20
							0x94, 0x8c, 0x76, 0x5a, 0x69, 0x14, 0xd4, 0x3f,
							0x2a, 0x7a, 0xc1, 0x77, 0xda, 0x2c, 0x2f, 0x6b,
							0x52, 0xde, 0x3d, 0x7c,
							0x88, // OP_EQUALVERIFY
							0xac, // OP_CHECKSIG
						},
					},
				},
			},
		},
	},
}

var scriptPubkey = Script{
	bytes: []byte{OP_DUP,
		OP_HASH160,
		0x14,
		0x71, 0xd7, 0xdd, 0x96, 0xd9, 0xed, 0xda, 0x09, 0x18, 0x0f,
		0xe9, 0xd5, 0x7a, 0x47, 0x7b, 0x5a, 0xcc, 0x9c, 0xad, 0x11,
		OP_EQUALVERIFY,
		OP_CHECKSIG,
	},
}

func TestSignatureHash(t *testing.T) {
	for _, test := range testsTx {
		tx := test.tx
		for i, in := range tx.Ins {
			hash, err := SignatureHash(&tx, in.Script, core.SIGHASH_ALL, i)
			if err != nil {
				t.Error(err)
			}
			fmt.Println(hash.ToString())
		}
	}

}

func TestTxHash(t *testing.T) {
	for _, test := range testsTx {
		tx := test.tx
		buf := new(bytes.Buffer)
		err := tx.Serialize(buf)
		if err != nil {
			t.Error(err)
		}
		txHash := core.DoubleSha256Hash(buf.Bytes())
		testHash := utils.HashFromString(test.hash)
		if !txHash.IsEqual(testHash) {
			t.Errorf(" tx hash (%s) error , is not %s", txHash.ToString(), test.hash)
		}
	}
}

func TestParseOpCode(t *testing.T) {
	for _, test := range testsTx {
		tx := test.tx
		for _, out := range tx.Outs {
			stk, err := out.Script.ParseScript()
			if err != nil {
				t.Error(err)
			}
			if len(stk) != 5 {
				t.Errorf("parse opcode is error , count is %d", len(stk))
			}

		}
		for _, in := range tx.Ins {
			stk, err := in.Script.ParseScript()
			if err != nil {
				t.Error(err)
			}
			if len(stk) != 2 {
				t.Errorf("parse opcode is error , count is %d", len(stk))
			}

		}
	}
}

func TestInterpreterVerify(t *testing.T) {
	//interpreter := Interpreter{
	//	stack: algorithm.NewStack(),
	//}
	//flag := core.SIGHASH_ALL
	//for _, test := range testsTx {
	//	tx := test.tx
	//	ret, err := interpreter.Verify(&tx, 0, tx.Ins[0].Script, &scriptPubkey, int32(flag))
	//	if err != nil {
	//		t.Error(err)
	//	} else {
	//		if !ret {
	//			t.Errorf("Tx Verify() fail")
	//		}
	//	}
	//
	//}

}
