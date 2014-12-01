package ate

import (
	//"encoding/json"
	"fmt"
	"github.com/eris-ltd/decerver-interfaces/core"
	"github.com/eris-ltd/decerver-interfaces/events"
	"github.com/robertkrimen/otto"
	"io/ioutil"
	"strings"
	"sync"
	"encoding/json"
)

type AteEventProcessor struct {
	er events.EventRegistry
}

type Ate struct {
	runtimes map[string]*JsRuntime
	apis     map[string]interface{}
	er       events.EventRegistry
}

func NewAte(er events.EventRegistry) *Ate {
	return &Ate{make(map[string]*JsRuntime), make(map[string]interface{}), er}
}

func (ate *Ate) ShutdownRuntimes() {
	for _, rt := range ate.runtimes {
		rt.Shutdown()
	}
}

func (ate *Ate) CreateRuntime(name string) core.Runtime {
	rt := newJsRuntime(name, ate.er)
	ate.runtimes[name] = rt
	rt.jsrEvents = NewJsrEvents(rt)
	// TODO add a "runtime" or "os" object with more stuff in it?

	rt.Init(name)
	for k, v := range ate.apis {
		// TODO error checking!
		rt.BindScriptObject(k, v)
	}
	fmt.Printf("Creating new runtime: " + name)
	// DEBUG
	fmt.Printf("Runtimes: %v\n", ate.runtimes)
	return rt
}

func (ate *Ate) GetRuntime(name string) core.Runtime {
	fmt.Println(name)
	fmt.Printf("Ate: %v\n", ate)
	return ate.runtimes[name]
}

func (ate *Ate) RemoveRuntime(name string) {
	ate.runtimes[name] = nil
}

func (ate *Ate) RegisterApi(name string, api interface{}) {
	ate.apis[name] = api
}

type JsRuntime struct {
	vm        *otto.Otto
	er        events.EventRegistry
	name      string
	jsrEvents *JsrEvents
	mutex     *sync.Mutex
	lockLvl	  int
}

func newJsRuntime(name string, er events.EventRegistry) *JsRuntime {
	vm := otto.New()
	jsr := &JsRuntime{}
	jsr.vm = vm
	jsr.er = er
	jsr.name = name
	jsr.mutex = &sync.Mutex{}
	return jsr
}

func (jsr *JsRuntime) Shutdown() {
	fmt.Println("Runtime shut down: " + jsr.name)
}

func (jsr *JsRuntime) lock(){
	jsr.mutex.Lock()
	jsr.lockLvl++
	fmt.Printf("[JSRuntime] Locking counter: %d\n",jsr.lockLvl);
	if jsr.lockLvl > 1 || jsr.lockLvl < 0 {
		panic("Lock level: weeeeird")
	}
}

func (jsr *JsRuntime) unlock(){
	jsr.mutex.Unlock()
	jsr.lockLvl--
	fmt.Printf("[JSRuntime] Locking counter: %d\n",jsr.lockLvl);
	if jsr.lockLvl > 1 || jsr.lockLvl < 0 {
		panic("Lock level: weeeeird")
	}
}

// TODO set up the interrupt channel.
func (jsr *JsRuntime) Init(name string) {
	jsr.vm.Set("jsr_events", jsr.jsrEvents)
	jsr.BindScriptObject("RuntimeId", name)
	BindDefaults(jsr)
}

func (jsr *JsRuntime) LoadScriptFile(fileName string) error {
	jsr.lock()
	defer jsr.unlock()
	bytes, err := ioutil.ReadFile(fileName)
	if err != nil {
		return err
	}
	_, err = jsr.vm.Run(bytes)
	return err
}

func (jsr *JsRuntime) LoadScriptFiles(fileName ...string) error {
	jsr.lock()
	defer jsr.unlock()
	for _, sf := range fileName {
		err := jsr.LoadScriptFile(sf)
		if err != nil {
			return err
		}
	}
	return nil
}

func (jsr *JsRuntime) BindScriptObject(name string, val interface{}) error {
	jsr.lock()
	defer jsr.unlock()
	err := jsr.vm.Set(name, val)
	return err
}

func (jsr *JsRuntime) AddScript(script string) error {
	jsr.lock()
	defer jsr.unlock()
	_, err := jsr.vm.Run(script)
	return err
}

func (jsr *JsRuntime) RunFunction(funcName string, params []string) (interface{}, error) {
	jsr.lock()
	defer jsr.unlock()
	cmd := funcName + "("

	paramStr := ""
	for _, p := range params {
		paramStr += p + ","
	}
	paramStr = strings.Trim(paramStr, ",")
	cmd += paramStr + ");"

	fmt.Println("Running function: " + cmd)
	val, runErr := jsr.vm.Run(cmd)

	if runErr != nil {
		return nil, fmt.Errorf("Error when running function '%s': %s\n", funcName, runErr.Error())
	}

	// Take the result and turn it into a go value.
	obj, expErr := val.Export()

	if expErr != nil {
		return nil, fmt.Errorf("Error when exporting returned value: %s\n", expErr.Error())
	}

	return obj, nil
}

func (jsr *JsRuntime) CallFuncOnObj(objName, funcName string, param ...interface{}) (interface{}, error) {
	jsr.lock()
	defer jsr.unlock()
	return jsr.cfoSafe(objName,funcName,param...)
}

func (jsr *JsRuntime) cfoSafe(objName, funcName string, param ...interface{}) (interface{}, error){
	defer func() {
        if err := recover(); err != nil {
            fmt.Println("work failed:", err)
        }
    }()
	ob, err := jsr.vm.Get(objName)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}
	
	val, callErr := ob.Object().Call(funcName, param...)

	if callErr != nil {
		fmt.Println(callErr.Error())
		return nil, err
	}

	// Take the result and turn it into a go value.
	obj, expErr := val.Export()

	if expErr != nil {
		return nil, fmt.Errorf("Error when exporting returned value: %s\n", expErr.Error())
	}
	return obj, nil
}

func (jsr *JsRuntime) CallFunc(funcName string, param ...interface{}) (interface{}, error) {
	jsr.lock()
	defer jsr.unlock()
	val, callErr := jsr.vm.Call(funcName, nil, param)

	if callErr != nil {
		fmt.Println(callErr.Error())
		return nil, callErr
	}

	fmt.Printf("%v\n", val)

	// Take the result and turn it into a go value.
	obj, expErr := val.Export()

	if expErr != nil {
		return nil, fmt.Errorf("Error when exporting returned value: %s\n", expErr.Error())
	}

	return obj, nil
}

// Use this to set up a new runtime. Should re-do init().
// TODO implement
func (jsr *JsRuntime) Recover() {
}

// Used to call the event processor from inside the javascript vm
type JsrEvents struct {
	jsr *JsRuntime
}

func NewJsrEvents(jsr *JsRuntime) *JsrEvents {
	return &JsrEvents{jsr}
}

func (jsre *JsrEvents) Subscribe(evtSource, evtType, evtTarget, subId string) {
	sub := NewAteSub(evtSource, evtType, evtTarget, subId, jsre.jsr)
	jsre.jsr.er.Subscribe(sub)
	// Launch the sub channel.
	go func(s *AteSub) {
		// DEBUG
		fmt.Println("Starting event loop for atesub: " + s.id)
		for {
			evt, ok := <-s.eventChan
			if !ok {
				fmt.Println("[Atë] Close message received.")
				return
			}
			fmt.Println("[Atë] stuff coming in from event processor: " + evt.Event)
			
			jsonString, err := json.Marshal(evt)
			// _ , err := json.Marshal(evt)
			if err != nil {
				fmt.Println("Error when posting event to ate: " + err.Error())
			}
			s.rt.CallFuncOnObj("events", "post", string(jsonString))
		}
	}(sub)
}

func (jsre *JsrEvents) Unsubscribe(subId string) {
	jsre.jsr.er.Unsubscribe(subId)
}

type AteSub struct {
	eventChan chan events.Event
	closeChan chan bool
	source    string
	tpe       string
	tgt       string
	id        string
	rt        core.Runtime
}

func NewAteSub(eventSource, eventType, eventTarget, subId string, rt core.Runtime) *AteSub {
	as := &AteSub{}
	as.closeChan = make(chan bool)
	as.source = eventSource
	as.tpe = eventType
	as.tgt = eventTarget
	as.id = subId
	as.rt = rt

	return as
}

func (as *AteSub) Channel() chan events.Event {
	return as.eventChan
}

func (as *AteSub) SetChannel(ec chan events.Event) {
	as.eventChan = ec
}

func (as *AteSub) Source() string {
	return as.source
}

func (as *AteSub) Id() string {
	return as.id
}

func (as *AteSub) Target() string {
	return as.tgt
}

func (as *AteSub) Event() string {
	return as.tpe
}
