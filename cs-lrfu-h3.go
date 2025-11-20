package table

import (
	"container/heap"
	"container/list"
	"fmt"
	"math"

	"github.com/named-data/ndnd/fw/defn"
)

// =========================
// Heap implementation
// =========================
type HeapEntry struct {
	index uint64
	crf   float64
	pos   int // posisi di heap
}

type MinHeap []*HeapEntry

func (h MinHeap) Len() int           { return len(h) }
func (h MinHeap) Less(i, j int) bool { return h[i].crf < h[j].crf }
func (h MinHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].pos = i
	h[j].pos = j
}
func (h *MinHeap) Push(x interface{}) {
	n := len(*h)
	item := x.(*HeapEntry)
	item.pos = n
	*h = append(*h, item)
}
func (h *MinHeap) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	item.pos = -1
	*h = old[0 : n-1]
	return item
}

// =========================
// CsLRFU policy
// =========================
type CsLRFU struct {
	cs        PitCsTable
	lambda    float64
	count     uint
	crf       map[uint64]float64
	lastRef   map[uint64]uint
	queue     *list.List
	locations map[uint64]*list.Element

	// tambahan heapList
	heapList MinHeap
	heapMap  map[uint64]*HeapEntry
}

func NewCsLRFU(cs PitCsTable, lambda float64) *CsLRFU {
	if lambda < 0.0 {
		lambda = 0.0
	} else if lambda > 1.0 {
		lambda = 1.0
	}

	return &CsLRFU{
		cs:        cs,
		lambda:    lambda,
		crf:       make(map[uint64]float64),
		lastRef:   make(map[uint64]uint),
		queue:     list.New(),
		locations: make(map[uint64]*list.Element),
		heapList:  MinHeap{},
		heapMap:   make(map[uint64]*HeapEntry),
	}
}

func (l *CsLRFU) getWeight(v uint) float64 {
	return math.Pow(0.5, l.lambda*float64(v))
}

func (l *CsLRFU) getCRF(index uint64) float64 {
	delta := l.count - l.lastRef[index]
	crfValue := l.getWeight(delta) * l.crf[index]
	return crfValue
}

// -------------------- AfterInsert --------------------
func (l *CsLRFU) AfterInsert(index uint64, wire []byte, data *defn.FwData) {
	l.count++
	delta := (100 - (100 - l.count))
	crfVal := l.getWeight(delta)
	l.crf[index] = crfVal
	l.lastRef[index] = l.count
	l.locations[index] = l.queue.PushBack(index)

	// masukkan ke heap
	entry := &HeapEntry{index: index, crf: crfVal}
	heap.Push(&l.heapList, entry)
	l.heapMap[index] = entry

	fmt.Printf("[CsLRFU] AfterInsert: index=%d | CRF=%.4f\n", index, crfVal)
}

// -------------------- AfterRefresh --------------------
func (l *CsLRFU) AfterRefresh(index uint64, wire []byte, data *defn.FwData) {
	l.count++
	weight := l.getWeight(0)
	if weight == 1.0 {
		l.crf[index] = weight
	} else {
		l.crf[index] = weight + l.getCRF(index)
	}
	l.lastRef[index] = l.count
	if loc, ok := l.locations[index]; ok {
		l.queue.Remove(loc)
	}
	l.locations[index] = l.queue.PushBack(index)

	// update heap
	if entry, ok := l.heapMap[index]; ok {
		entry.crf = l.crf[index]
		heap.Fix(&l.heapList, entry.pos)
	}
}

// -------------------- BeforeErase --------------------
func (l *CsLRFU) BeforeErase(index uint64, wire []byte) {
	if loc, ok := l.locations[index]; ok {
		l.queue.Remove(loc)
	}
	delete(l.crf, index)
	delete(l.lastRef, index)
	delete(l.locations, index)

	// hapus dari heap
	if entry, ok := l.heapMap[index]; ok {
		heap.Remove(&l.heapList, entry.pos)
		delete(l.heapMap, index)
	}

	fmt.Printf("[CsLRFU] BeforeErase: index=%d dihapus\n", index)
}

// -------------------- BeforeUse --------------------
func (l *CsLRFU) BeforeUse(index uint64, wire []byte) {
	l.count++
	weight := l.getWeight(0)
	if weight == 1.0 {
		l.crf[index] = weight
	} else {
		l.crf[index] = weight + l.getCRF(index)
	}
	l.lastRef[index] = l.count
	if loc, ok := l.locations[index]; ok {
		l.queue.Remove(loc)
	}
	l.locations[index] = l.queue.PushBack(index)

	// update heap
	if entry, ok := l.heapMap[index]; ok {
		entry.crf = l.crf[index]
		heap.Fix(&l.heapList, entry.pos)
	}

	fmt.Printf("[CsLRFU] BeforeUse: index=%d updated CRF=%.4f\n", index, l.crf[index])
}

// -------------------- EvictEntries --------------------
func (l *CsLRFU) EvictEntries() {
	for l.queue.Len() > CfgCsCapacity() {
		if l.heapList.Len() == 0 {
			fmt.Println("[CsLRFU] EvictEntries: heap kosong, stop")
			break
		}
		// ambil CRF terkecil dari heap
		item := heap.Pop(&l.heapList).(*HeapEntry)
		targetIndex := item.index
		minCRF := item.crf

		// hapus dari semua struktur
		if loc, ok := l.locations[targetIndex]; ok {
			l.queue.Remove(loc)
		}
		delete(l.crf, targetIndex)
		delete(l.lastRef, targetIndex)
		delete(l.locations, targetIndex)
		delete(l.heapMap, targetIndex)

		l.cs.eraseCsDataFromReplacementStrategy(targetIndex)

		fmt.Printf("[CsLRFU] EvictEntries: index=%d dengan CRF=%.4f dihapus\n", targetIndex, minCRF)
	}
}

