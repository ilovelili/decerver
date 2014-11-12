package deCerver

import (
	"fmt"
	"github.com/eris-ltd/deCerver-interfaces/modules"
	"github.com/eris-ltd/deCerver-interfaces/core"
	"github.com/eris-ltd/deCerver/ate"
	"github.com/eris-ltd/deCerver/events"
	"github.com/eris-ltd/deCerver/moduleregistry"
	"github.com/eris-ltd/deCerver/dappregistry"
	"github.com/eris-ltd/deCerver/server"
	"io/ioutil"
	"os"
	"os/signal"
	"path"
)

type Paths struct {
	root        string
	modules     string
	log         string
	blockchains string
	filesystems string
	apps        string
}

func (p *Paths) Root() string {
	return p.root
}

func (p *Paths) Modules() string {
	return p.modules
}

func (p *Paths) Log() string {
	return p.log
}

func (p *Paths) Apps() string {
	return p.apps
}

func (p *Paths) Blockchains() string {
	return p.blockchains
}

func (p *Paths) Filesystems() string {
	return p.filesystems
}

func (p *Paths) ReadFile(directory, name string) ([]byte, error) {
	if directory[len(directory) - 1] != '/' {
		directory += "/"
	}
	return ioutil.ReadFile((path.Join(directory,name)))
}


func (p *Paths) WriteFile(directory, name string, data []byte) error {
	if directory[len(directory) - 1] != '/' {
		directory += "/"
	}
	return ioutil.WriteFile((path.Join(directory,name)),data,0600)
}	

// Creates a new directory for a module, and returns the path.
func (p *Paths) CreateDirectory(moduleName string) string {
	dir := p.modules + "/" + moduleName
	InitDir(dir)
	return dir
}

type DeCerver struct {
	config         *core.DCConfig
	paths          *Paths
	ep             *events.EventProcessor
	ate            *ate.Ate
	webServer      *server.WebServer
	moduleRegistry *moduleregistry.ModuleRegistry
	dappRegistry   *dappregistry.DappRegistry
}

func NewDeCerver() *DeCerver {
	dc := &DeCerver{}
	fmt.Println("Starting decerver bootstrapping sequence.")
	dc.ReadConfig("")
	dc.createPaths()
	dc.WriteConfig(dc.config)
	server.Init(dc)
	dc.createNetwork()
	dc.createAte()
	dc.createEventProcessor()
	dc.initAte()
	dc.createModuleRegistry()
	dc.createDappRegistry()
	return dc
}

func (dc *DeCerver) Init() {
	err := dc.moduleRegistry.Init()
	dc.ep.Subscribe(dc.ate)
	if err != nil {
		fmt.Printf("Module failed to load: %s. Shutting down.\n", err.Error())
		os.Exit(-1)
	}
	dc.initDapps()
}

func (dc *DeCerver) Start() {
	dc.webServer.Start()
	fmt.Println("Server started.")

	err := dc.moduleRegistry.Start()
	if err != nil {
		fmt.Printf("Module failed to start: %s. Shutting down.\n", err.Error())
		os.Exit(-1)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)
	<-c
	fmt.Println("Shutting down")
}

func (dc *DeCerver) createPaths() {
	dc.paths = &Paths{}
	dc.paths.root = dc.config.RootDir
	InitDir(dc.paths.root)
	dc.paths.log = dc.paths.root + "/logs"
	InitDir(dc.paths.log)
	dc.paths.modules = dc.paths.root + "/modules"
	InitDir(dc.paths.modules)
	dc.paths.apps = dc.paths.root + "/apps"
	InitDir(dc.paths.apps)
	dc.paths.filesystems = dc.paths.root + "/filesystems"
	InitDir(dc.paths.apps)
	dc.paths.blockchains = dc.paths.root + "/blockchains"
	InitDir(dc.paths.apps)
}

func (dc *DeCerver) createNetwork() {
	dc.webServer = server.NewWebServer(uint32(dc.config.MaxClients), dc.paths.Apps(),dc.config.Port)
}

func (dc *DeCerver) createEventProcessor() {
	dc.ep = events.NewEventProcessor()
}

func (dc *DeCerver) createAte() {
	dc.ate = ate.NewAte(dc.ep)
}

func (dc *DeCerver) initAte() {
	dc.ate.Init()
}

func (dc *DeCerver) createModuleRegistry() {
	dc.moduleRegistry = moduleregistry.NewModuleRegistry()
}

func (dc *DeCerver) createDappRegistry() {
	dc.dappRegistry = dappregistry.NewDappRegistry()
}

func (dc *DeCerver) LoadModule(md modules.Module) {
	md.Register(nil, dc.webServer, dc.ate, dc.ep)
	dc.moduleRegistry.Add(md)
	fmt.Printf("Registering module '%s'.\n", md.Name())
}

func (dc *DeCerver) initDapps() {
	fmt.Println("Loading dapps")
	err := dc.dappRegistry.LoadDapps(dc.paths.Apps())
	
	if err != nil {
		fmt.Println("Error loading dapps: " + err.Error())
		os.Exit(0)
	}
}

func (dc *DeCerver) initDapp(){
	
}

func (dc *DeCerver) GetPaths() core.FileIO {
	return dc.paths
}