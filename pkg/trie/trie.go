/**
 *  @file
 *  @copyright defined in aergo/LICENSE.txt
 */

package trie

// The Package Trie implements a sparse merkle trie.

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/aergoio/aergo/pkg/db"
)

const (
	HashLength   = 32
	maxPastTries = 300
)

// TODO make a secure trie that hashes keys with a random seed to be sure the trie is sparse.

// SMT is a sparse Merkle tree.
type SMT struct {
	db *CacheDB
	// Root is the current root of the smt.
	Root []byte
	// lock is for the whole struct
	lock sync.RWMutex
	hash func(data ...[]byte) []byte
	// KeySize is the size in bytes corresponding to TrieHeight, is size of an address.
	KeySize       uint64
	TrieHeight    uint64
	defaultHashes [][]byte
	// LoadDbCounter counts the nb of db reads in on update
	LoadDbCounter uint64
	// loadDbMux is a lock for LoadDbCounter
	loadDbMux sync.RWMutex
	// LoadCacheCounter counts the nb of cache reads in on update
	LoadCacheCounter uint64
	// liveCountMux is a lock fo LoadCacheCounter
	liveCountMux sync.RWMutex
	// CacheHeightLimit is the number of tree levels we want to store in cache
	CacheHeightLimit uint64
	// pastTries stores the past maxPastTries trie roots to revert
	pastTries [][]byte
}

// NewSMT creates a new SMT given a keySize and a hash function.
func NewSMT(keySize uint64, hash func(data ...[]byte) []byte, store db.DB) *SMT {
	s := &SMT{
		hash:             hash,
		TrieHeight:       keySize * 8,
		CacheHeightLimit: 232, //246, //234, // based on the number of nodes we can keep in memory.
		KeySize:          keySize,
	}
	s.db = &CacheDB{
		liveCache:    make(map[Hash][]byte, 5e6),
		updatedNodes: make(map[Hash][]byte, 5e3),
		store:        store,
	}
	s.Root = s.loadDefaultHashes()
	return s
}

// loadDefaultHashes creates the default hashes and stores them in cache
func (s *SMT) loadDefaultHashes() []byte {
	s.defaultHashes = make([][]byte, s.TrieHeight+1)
	s.defaultHashes[0] = DefaultLeaf
	var h []byte
	var node Hash
	for i := 1; i <= int(s.TrieHeight); i++ {
		h = s.hash(s.defaultHashes[i-1], s.defaultHashes[i-1])
		copy(node[:], h)
		s.defaultHashes[i] = h
		// default hashes are always in livecache and don't need to be stored to disk
		s.db.liveCache[node] = append(s.defaultHashes[i-1], append(s.defaultHashes[i-1], byte(0))...)
	}
	return h
}

// Update adds a sorted list of keys and their values to the trie
func (s *SMT) Update(keys, values DataArray) ([]byte, error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.LoadDbCounter = 0
	s.LoadCacheCounter = 0
	update, err := s.update(s.Root, keys, values, s.TrieHeight, false, true, nil)
	if err != nil {
		return nil, err
	}
	s.Root = update
	return s.Root, err
}

// result is used to contain the result of goroutines and is sent through a channel.
type result struct {
	update []byte
	err    error
}

// update adds a sorted list of keys and their values to the trie.
// It returns the root of the updated tree.
func (s *SMT) update(root []byte, keys, values DataArray, height uint64, shortcut, store bool, ch chan<- (result)) ([]byte, error) {
	if height == 0 {
		return values[0], nil
	}
	lnode, rnode, isShortcut, err := s.loadChildren(root)
	if err != nil {
		if ch != nil {
			ch <- result{nil, err}
		}
		return nil, err
	}
	if isShortcut == 1 {
		if !bytes.Equal(keys[0], lnode) || len(keys) != 1 {
			// this is called if a new account is being added
			keys, values = s.addShortcutToDataArray(keys, values, lnode, rnode)
		}
		// The shortcut node was added to keys and values so consider this subtree default.
		lnode, rnode = s.defaultHashes[height-1], s.defaultHashes[height-1]
	}

	// Split the keys array so each branch can be updated in parallel
	lkeys, rkeys := s.splitKeys(keys, s.TrieHeight-height)
	splitIndex := len(lkeys)
	lvalues, rvalues := values[:splitIndex], values[splitIndex:]

	if shortcut {
		store = false    //stop storing only after the shortcut node.
		shortcut = false // remove shortcut node flag
	}
	if bytes.Equal(s.defaultHashes[height-1], lnode) && bytes.Equal(s.defaultHashes[height-1], rnode) && (len(keys) == 1) && store {
		// if the subtree contains only one key, store the key/value in a shortcut node
		shortcut = true
	}
	switch {
	case lkeys.Len() == 0 && rkeys.Len() > 0:
		// all the keys go in the right subtree
		update, err := s.update(rnode, keys, values, height-1, shortcut, store, nil)
		if err != nil {
			if ch != nil {
				ch <- result{nil, err}
			}
			return nil, err
		}
		// if this update() call is a goroutine, return the result through the channel
		if ch != nil {
			ch <- result{s.interiorHash(lnode, update, height-1, root, shortcut, store, keys, values), nil}
			return nil, nil
		}
		return s.interiorHash(lnode, update, height-1, root, shortcut, store, keys, values), nil
	case lkeys.Len() > 0 && rkeys.Len() == 0:
		// all the keys go in the left subtree
		update, err := s.update(lnode, keys, values, height-1, shortcut, store, nil)
		if err != nil {
			if ch != nil {
				ch <- result{nil, err}
			}
			return nil, err
		}
		// if this update() call is a goroutine, return the result through the channel
		if ch != nil {
			ch <- result{s.interiorHash(update, rnode, height-1, root, shortcut, store, keys, values), nil}
			return nil, nil
		}
		return s.interiorHash(update, rnode, height-1, root, shortcut, store, keys, values), nil
	default:
		if height < 240 { // 16 height~= 65000 nodes / goroutines
			// FIXME : if all the accounts being updated have the same 16 first bits then goroutines are useless here. Check for number of running goroutines instead.
			lupdate, err := s.update(lnode, lkeys, lvalues, height-1, shortcut, store, nil)
			if err != nil {
				if ch != nil {
					ch <- result{nil, err}
				}
				return nil, err
			}
			rupdate, err := s.update(rnode, rkeys, rvalues, height-1, shortcut, store, nil)
			if err != nil {
				if ch != nil {
					ch <- result{nil, err}
				}
				return nil, err
			}
			if ch != nil {
				ch <- result{s.interiorHash(lupdate, rupdate, height-1, root, shortcut, store, keys, values), nil}
				return nil, nil
			}
			return s.interiorHash(lupdate, rupdate, height-1, root, shortcut, store, keys, values), nil
		}

		// keys are separated between the left and right branches
		// update the branches in parallel
		lch := make(chan result, 1)
		rch := make(chan result, 1)
		go s.update(lnode, lkeys, lvalues, height-1, shortcut, store, lch)
		go s.update(rnode, rkeys, rvalues, height-1, shortcut, store, rch)
		lresult := <-lch
		rresult := <-rch
		if lresult.err != nil {
			if ch != nil {
				ch <- result{nil, lresult.err}
			}
			return nil, lresult.err
		}
		if rresult.err != nil {
			if ch != nil {
				ch <- result{nil, rresult.err}
			}
			return nil, rresult.err
		}
		// if this update() call is a goroutine, return the result through the channel
		if ch != nil {
			ch <- result{s.interiorHash(lresult.update, rresult.update, height-1, root, shortcut, store, keys, values), nil}
			return nil, nil
		}
		return s.interiorHash(lresult.update, rresult.update, height-1, root, shortcut, store, keys, values), nil
	}
}

// splitKeys devides the array of keys into 2 so they can update left and right branches in parallel
func (s *SMT) splitKeys(keys DataArray, height uint64) (DataArray, DataArray) {
	for i, key := range keys {
		if bitIsSet(key, height) {
			return keys[:i], keys[i:]
		}
	}
	return keys, nil
}

// addShortcutToDataArray adds a shortcut key to the keys array to be updated.
// this is used when a subtree containing a shortcut node is being updated
func (s *SMT) addShortcutToDataArray(keys, values DataArray, shortcutKey, shortcutVal []byte) (DataArray, DataArray) {
	newKeys := make(DataArray, 0, len(keys)+1)
	newVals := make(DataArray, 0, len(keys)+1)

	if bytes.Compare(shortcutKey, keys[0]) < 0 {
		newKeys = append(newKeys, shortcutKey)
		newKeys = append(newKeys, keys...)
		newVals = append(newVals, shortcutVal)
		newVals = append(newVals, values...)
	} else if bytes.Compare(shortcutKey, keys[len(keys)-1]) > 0 {
		newKeys = append(newKeys, keys...)
		newKeys = append(newKeys, shortcutKey)
		newVals = append(newVals, values...)
		newVals = append(newVals, shortcutVal)
	} else {
		higher := false
		for i, key := range keys {
			if bytes.Equal(shortcutKey, key) {
				return keys, values
			}
			if bytes.Compare(shortcutKey, key) > 0 {
				higher = true
			}
			if higher && bytes.Compare(shortcutKey, key) < 0 {
				// insert shortcut in slices
				newKeys = append(newKeys, keys[:i]...)
				newKeys = append(newKeys, shortcutKey)
				newKeys = append(newKeys, keys[i:]...)
				newVals = append(newVals, values[:i]...)
				newVals = append(newVals, shortcutVal)
				newVals = append(newVals, values[i:]...)
				return newKeys, newVals
			}
		}
	}
	return newKeys, newVals
}

// loadChildren looks for the children of a node.
// if the node is not stored in cache, it will be loaded from db.
func (s *SMT) loadChildren(root []byte) ([]byte, []byte, byte, error) {
	var node Hash
	copy(node[:], root)

	s.db.liveMux.RLock()
	val, exists := s.db.liveCache[node]
	s.db.liveMux.RUnlock()
	if exists {
		s.liveCountMux.Lock()
		s.LoadCacheCounter++
		s.liveCountMux.Unlock()
		nodeSize := len(val)
		shortcut := val[nodeSize-1]
		if shortcut == 1 {
			return val[:s.KeySize], val[s.KeySize : nodeSize-1], shortcut, nil
		}
		return val[:HashLength], val[HashLength : nodeSize-1], shortcut, nil
	}

	// checking updated nodes is useful if get() or update() is called twice in a row without db commit
	s.db.updatedMux.RLock()
	val, exists = s.db.updatedNodes[node]
	s.db.updatedMux.RUnlock()
	if exists {
		nodeSize := len(val)
		shortcut := val[nodeSize-1]
		if shortcut == 1 {
			return val[:s.KeySize], val[s.KeySize : nodeSize-1], shortcut, nil
		}
		return val[:HashLength], val[HashLength : nodeSize-1], shortcut, nil
	}
	//Fetch node in disk database
	s.loadDbMux.Lock()
	s.LoadDbCounter++
	s.loadDbMux.Unlock()
	s.db.lock.Lock()
	val = s.db.store.Get(root)
	s.db.lock.Unlock()
	nodeSize := len(val)
	if nodeSize != 0 {
		shortcut := val[nodeSize-1]
		if shortcut == 1 {
			return val[:s.KeySize], val[s.KeySize : nodeSize-1], shortcut, nil
		}
		return val[:HashLength], val[HashLength : nodeSize-1], shortcut, nil
	}
	return nil, nil, byte(0), fmt.Errorf("the trie node %x is unavailable in the disk db, db may be corrupted", root)
}

// Get fetches the value of a key by going down the current trie root.
func (s *SMT) Get(key []byte) ([]byte, error) {
	return s.get(s.Root, key, s.TrieHeight)
}

// get fetches the value of a key given a trie root
func (s *SMT) get(root []byte, key []byte, height uint64) ([]byte, error) {
	if height == 0 {
		return root, nil
	}
	// Fetch the children of the node
	lnode, rnode, isShortcut, err := s.loadChildren(root)
	if err != nil {
		return nil, err
	}
	if isShortcut == 1 {
		if bytes.Equal(lnode, key) {
			return rnode, nil
		}
		return nil, nil
	}
	if bitIsSet(key, s.TrieHeight-height) {
		return s.get(rnode, key, height-1)
	}
	return s.get(lnode, key, height-1)
}

// DefaultHash is a getter for the defaultHashes array
func (s *SMT) DefaultHash(height uint64) []byte {
	return s.defaultHashes[height]
}

// interiorHash hashes 2 children to get the parent hash and stores it in the updatedNodes and maybe in liveCache.
// the key is the hash and the value is the appended child nodes or the appended key/value in case of a shortcut.
// keys of go mappings cannot be byte slices so the hash is copied to a byte array
func (s *SMT) interiorHash(left, right []byte, height uint64, oldRoot []byte, shortcut, store bool, keys, values DataArray) []byte {
	h := s.hash(left, right)
	var node Hash
	copy(node[:], h)
	if store {
		if !shortcut {
			children := make([]byte, 0, HashLength*2+1)
			children = append(children, left...)
			children = append(children, right...)
			children = append(children, byte(0))
			// Cache the node if it's children are not default and if it's height is over CacheHeightLimit
			if (!bytes.Equal(s.defaultHashes[height], left) ||
				!bytes.Equal(s.defaultHashes[height], right)) &&
				height > s.CacheHeightLimit {
				s.db.liveMux.Lock()
				s.db.liveCache[node] = children
				s.db.liveMux.Unlock()
			}
			// store new node in db
			s.db.updatedMux.Lock()
			s.db.updatedNodes[node] = children
			s.db.updatedMux.Unlock()

		} else {
			// shortcut is only true if len(keys)==1
			kv := make([]byte, 0, s.KeySize+HashLength+1)
			kv = append(kv, keys[0]...)
			kv = append(kv, values[0]...)
			kv = append(kv, byte(1))
			// Cache the shortcut node if it's children are not default and if it's height is over CacheHeightLimit
			if (!bytes.Equal(s.defaultHashes[height], left) ||
				!bytes.Equal(s.defaultHashes[height], right)) &&
				height > s.CacheHeightLimit {
				s.db.liveMux.Lock()
				s.db.liveCache[node] = kv
				s.db.liveMux.Unlock()
			}
			// store new node in db
			s.db.updatedMux.Lock()
			s.db.updatedNodes[node] = kv
			s.db.updatedMux.Unlock()
		}
		if !bytes.Equal(s.defaultHashes[height+1], oldRoot) && !bytes.Equal(h, oldRoot) {
			// Delete old liveCache node if it has been updated and is not default
			var node Hash
			copy(node[:], oldRoot)
			s.db.liveMux.Lock()
			delete(s.db.liveCache, node)
			s.db.liveMux.Unlock()
			//NOTE this could delete a node used by another part of the tree if some values are equal.
		}
	}
	return h
}

// Commit stores the updated nodes to disk
func (s *SMT) Commit() {
	// Commit the new nodes to database, clear updatedNodes and store the Root in history for reverts.
	if len(s.pastTries) >= maxPastTries {
		copy(s.pastTries, s.pastTries[1:])
		s.pastTries[len(s.pastTries)-1] = s.Root
	} else {
		s.pastTries = append(s.pastTries, s.Root)
	}
	s.db.commit()
	s.db.updatedNodes = make(map[Hash][]byte, len(s.db.updatedNodes)*2)
}
