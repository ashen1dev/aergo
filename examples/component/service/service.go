/**
 *  @file
 *  @copyright defined in aergo/LICENSE.txt
 */

package service

import (
	"fmt"

	"github.com/AsynkronIT/protoactor-go/actor"
	"github.com/aergoio/aergo/examples/component/message"
	"github.com/aergoio/aergo/pkg/component"
	"github.com/aergoio/aergo/pkg/log"
)

type ExampleService struct {
	*component.BaseComponent
	myname string
}

var _ component.IComponent = (*ExampleService)(nil)

func NexExampleServie(myname string) *ExampleService {
	return &ExampleService{
		BaseComponent: component.NewBaseComponent(message.HelloService, log.NewLogger(log.TEST), true),
		myname:        myname,
	}
}

func (es *ExampleService) Start() {
	es.BaseComponent.Start(es)
	//TODO add init logics for this service
}

func (es *ExampleService) Stop() {
	es.BaseComponent.Stop()
	//TODO add stop logics for this service
}

func (es *ExampleService) Receive(context actor.Context) {
	es.BaseComponent.Receive(context)

	switch msg := context.Message().(type) {
	case *message.HelloReq:
		context.Respond(
			&message.HelloRsp{
				Greeting: fmt.Sprintf("Hello %s, I'm %s", msg.Who, es.myname),
			})
	}
}
