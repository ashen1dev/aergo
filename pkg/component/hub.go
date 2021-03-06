/**
 *  @file
 *  @copyright defined in aergo/LICENSE.txt
 */

package component

import (
	"sync"
	"time"

	"github.com/AsynkronIT/protoactor-go/actor"
)

// ICompSyncRequester is the interface that wraps the RequestFuture method.
type ICompSyncRequester interface {
	RequestFuture(targetName string, message interface{}, timeout time.Duration) *actor.Future
}

type ComponentHub struct {
	components map[string]IComponent
}

type hubInitSync struct {
	sync.WaitGroup
	finished chan interface{}
}

var hubInit hubInitSync

func NewComponentHub() *ComponentHub {
	hub := ComponentHub{
		components: make(map[string]IComponent),
	}

	return &hub
}

func (h *hubInitSync) begin(n int) {
	h.finished = make(chan interface{})
	h.Add(n)
}

func (h *hubInitSync) end() {
	h.Wait()
	close(h.finished)
}

func (h *hubInitSync) wait() {
	h.Done()
	<-h.finished
}

func (hub *ComponentHub) Start() {
	hubInit.begin(len(hub.components))
	for _, comp := range hub.components {
		go comp.Start()
	}
	hubInit.end()
}

func (hub *ComponentHub) Stop() {
	for _, comp := range hub.components {
		comp.Stop()
	}
}

func (hub *ComponentHub) Register(component IComponent) {
	hub.components[component.GetName()] = component
	component.SetHub(hub)
}

func (hub *ComponentHub) Status() (map[string]*StatusRsp, error) {
	var retCompStatus map[string]*StatusRsp
	retCompStatus = make(map[string]*StatusRsp)

	for _, comp := range hub.components {
		// todo Masure time spent and add to result
		rawResponse, err := comp.RequestFuture(&StatusReq{}, time.Second*10).Result()
		if err != nil {
			return nil, err
		}
		retCompStatus[comp.GetName()] = rawResponse.(*StatusRsp)
	}

	return retCompStatus, nil
}

func (hub *ComponentHub) Request(targetName string, message interface{}, sender IComponent) {
	targetComponent := hub.components[targetName]
	if targetComponent == nil {
		panic("Unregistered Component")
	}

	targetComponent.Request(message, sender)
}

func (hub *ComponentHub) RequestFuture(
	targetName string, message interface{}, timeout time.Duration) *actor.Future {

	targetComponent := hub.components[targetName]
	if targetComponent == nil {
		panic("Unregistered Component")
	}

	return targetComponent.RequestFuture(message, timeout)
}
