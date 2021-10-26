package main

import (
	"encoding/json"
	"github.com/beeker1121/goque"
	"go.uber.org/zap"
	"time"
)

type scrapUniqueProductQueue struct {
	DBAssetID uint32
	UID       string
}
type scrapUniqueProduct struct {
	UID string `json:"UID"`
}

type ScrapUniqueProductHandler struct {
	priorityQueue *goque.PriorityQueue
	shutdown      bool
}

func NewScrapUniqueProductHandler() (handler *ScrapUniqueProductHandler) {
	const queuePathDB = "/data/ScrapUniqueProduct"
	var priorityQueue *goque.PriorityQueue
	var err error
	priorityQueue, err = SetupQueue(queuePathDB)
	if err != nil {
		zap.S().Errorf("Error setting up remote queue (%s)", queuePathDB, err)
		zap.S().Errorf("err: %s", err)
		ShutdownApplicationGraceful()
		panic("Failed to setup queue, exiting !")
	}

	handler = &ScrapUniqueProductHandler{
		priorityQueue: priorityQueue,
		shutdown:      false,
	}
	return
}

func (r ScrapUniqueProductHandler) reportLength() {
	for !r.shutdown {
		time.Sleep(10 * time.Second)
		if r.priorityQueue.Length() > 0 {
			zap.S().Debugf("ScrapUniqueProductHandler queue length: %d", r.priorityQueue.Length())
		}
	}
}
func (r ScrapUniqueProductHandler) Setup() {
	go r.reportLength()
	go r.process()
}
func (r ScrapUniqueProductHandler) process() {
	var items []*goque.PriorityItem
	for !r.shutdown {
		items = r.dequeue()
		if len(items) == 0 {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		faultyItems, err := storeItemsIntoDatabaseUniqueProductScrap(items)
		if err != nil {
			zap.S().Errorf("err: %s", err)
			ShutdownApplicationGraceful()
			return
		}
		// Empty the array, without de-allocating memory
		items = items[:0]
		for _, faultyItem := range faultyItems {
			var prio uint8
			prio = faultyItem.Priority + 1
			if faultyItem.Priority >= 255 {
				prio = 254
			}
			r.enqueue(faultyItem.Value, prio)
		}
	}
}

func (r ScrapUniqueProductHandler) dequeue() (items []*goque.PriorityItem) {
	if r.priorityQueue.Length() > 0 {
		item, err := r.priorityQueue.Dequeue()
		if err != nil {
			return
		}
		items = append(items, item)

		for true {
			nextItem, err := r.priorityQueue.DequeueByPriority(item.Priority)
			if err != nil {
				break
			}
			items = append(items, nextItem)
		}
	}
	return
}

func (r ScrapUniqueProductHandler) enqueue(bytes []byte, priority uint8) {
	_, err := r.priorityQueue.Enqueue(priority, bytes)
	if err != nil {
		zap.S().Warnf("Failed to enqueue item", bytes, err)
		return
	}
}

func (r ScrapUniqueProductHandler) Shutdown() (err error) {
	zap.S().Warnf("[ScrapUniqueProductHandler] shutting down, Queue length: %d", r.priorityQueue.Length())
	r.shutdown = true
	time.Sleep(5 * time.Second)
	err = CloseQueue(r.priorityQueue)
	return
}

func (r ScrapUniqueProductHandler) EnqueueMQTT(customerID string, location string, assetID string, payload []byte) {
	zap.S().Debugf("[ScrapUniqueProductHandler]")
	var parsedPayload scrapUniqueProduct

	err := json.Unmarshal(payload, &parsedPayload)
	if err != nil {
		zap.S().Errorf("json.Unmarshal failed", err, payload)
		return
	}

	DBassetID := GetAssetID(customerID, location, assetID)
	newObject := scrapUniqueProductQueue{
		UID:       parsedPayload.UID,
		DBAssetID: DBassetID,
	}

	marshal, err := json.Marshal(newObject)
	if err != nil {
		return
	}

	r.enqueue(marshal, 0)
	return
}