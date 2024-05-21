package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	log "github.com/sirupsen/logrus"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
	"github.com/flant/addon-operator/pkg/module_manager/scheduler/node"
)

type Scheduler struct {
	ctx context.Context

	// list of extenders to cycle over on a run
	extenders []extenders.Extender
	extCh     chan extenders.ExtenderEvent

	l sync.Mutex
	// directed acyclic graph consisting of vertices representing modules and weights
	dag graph.Graph[string, *node.Node]
	// the root vertex of the dag
	root *node.Node
	// cache containing currently enabled vertices
	enabledModules *[]string
	// cache constisting of the extenders' results from the previous run
	retrospectiveStatus map[extenders.ExtenderName]map[string]bool
}

func NewScheduler(ctx context.Context) *Scheduler {
	nodeHash := func(n *node.Node) string {
		return n.GetName()
	}
	return &Scheduler{
		ctx:                 ctx,
		extenders:           make([]extenders.Extender, 0),
		extCh:               make(chan extenders.ExtenderEvent, 1),
		dag:                 graph.New(nodeHash, graph.Directed(), graph.Acyclic()),
		retrospectiveStatus: make(map[extenders.ExtenderName]map[string]bool),
	}
}

func (s *Scheduler) EventCh() chan extenders.ExtenderEvent {
	return s.extCh
}

func (s *Scheduler) printGraph() {
	file, err := os.Create("./external-modules/node.gv")
	if err != nil {
		log.Errorf("Couldn't create graph file: %v", err)
	}
	defer file.Close()

	err = draw.DOT(s.dag, file)
	if err != nil {
		log.Errorf("Couldn't draw graph: %v", err)
	}

	report, err := s.PrintSummary()
	if err != nil {
		log.Errorf("Couldn't get the report: %v", err)
	}

	b, err := json.Marshal(report)
	if err != nil {
		log.Errorf("Couldn't marshal the report: %v", err)
	}

	err = os.WriteFile("./external-modules/report", b, 0o644)
	if err != nil {
		log.Errorf("Couldn't write the report: %v", err)
	}
}

func (s *Scheduler) AddModuleVertex(module node.ModuleInterface) error {
	vertex := node.NewNode().WithName(module.GetName()).WithWeight(module.GetOrder()).WithType(node.ModuleType).WithModule(module)
	// add module vertex
	if err := s.dag.AddVertex(vertex, graph.VertexAttribute("colorscheme", "greens3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"), graph.VertexAttribute("type", string(node.ModuleType))); err != nil {
		return err
	}

	// check if pertaining weight vertex already exists
	parent, err := s.dag.Vertex(vertex.GetWeight().String())
	switch {
	// parent found
	case err == nil:
		if err := s.dag.AddEdge(parent.GetName(), vertex.GetName()); err != nil {
			return fmt.Errorf("Couldn't add an edge between %s and %s vertices: %w", parent.GetName(), vertex.GetName(), err)
		}
	// some other error
	case !errors.Is(err, graph.ErrVertexNotFound):
		return err
	// parent not found - create it
	default:
		parent = node.NewNode().WithName(vertex.GetWeight().String()).WithWeight(uint32(vertex.GetWeight())).WithType(node.WeightType)
		if err := s.AddWeightVertex(parent); err != nil {
			return err
		}
		if err := s.dag.AddEdge(parent.GetName(), vertex.GetName()); err != nil {
			return fmt.Errorf("Couldn't add an edge between %s and %s vertices: %w", parent.GetName(), vertex.GetName(), err)
		}
	}
	return nil
}

func (s *Scheduler) AddWeightVertex(vertex *node.Node) error {
	if err := s.dag.AddVertex(vertex, graph.VertexWeight(int(vertex.GetWeight())), graph.VertexAttribute("colorscheme", "blues3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"), graph.VertexAttribute("type", string(node.WeightType))); err != nil {
		return err
	}

	if s.root == nil {
		s.root = vertex
		return nil
	}

	var (
		parent *node.Node
		child  *node.Node
		dfsErr error
	)
	err := graph.DFS(s.dag, s.root.GetName(), func(name string) bool {
		currVertex, props, err := s.dag.VertexWithProperties(name)
		if err != nil {
			dfsErr = fmt.Errorf("Couldn't get %s vertex from the graph: %v", name, err)
			return true
		}

		// filter out module vertices
		if props.Attributes["type"] == string(node.ModuleType) {
			return false
		}

		// possible parent found
		if props.Weight < vertex.GetWeight().Int() {
			parent = currVertex
		}

		// a child found
		if child == nil && props.Weight > vertex.GetWeight().Int() {
			child = currVertex
		}

		return false
	})
	if err != nil {
		return err
	}

	if dfsErr != nil {
		return dfsErr
	}

	if parent != nil {
		if err := s.dag.AddEdge(parent.GetName(), vertex.GetName()); err != nil {
			return fmt.Errorf("Couldn't add an edge between %s and %s vertices: %w", parent.GetName(), vertex.GetName(), err)
		}

		if child != nil {
			// insert a new vertex between two existing ones
			if err := s.dag.RemoveEdge(parent.GetName(), child.GetName()); err != nil {
				return fmt.Errorf("Couldn't delete an existing edge between %s and %s vertices: %w", parent.GetName(), child.GetName(), err)
			}
		}
	}

	if child != nil {
		if err := s.dag.AddEdge(vertex.GetName(), child.GetName()); err != nil {
			return fmt.Errorf("Couldn't add an edge between %s and %s vertices: %w", vertex.GetName(), child.GetName(), err)
		}
	}

	if vertex.GetWeight() < s.root.GetWeight() {
		s.root = vertex
	}

	return nil
}

func (s *Scheduler) AddExtender(ext extenders.Extender) error {
	for _, ex := range s.extenders {
		if ex.Name() == ext.Name() {
			return fmt.Errorf("Extender %s already added", ext.Name())
		}
	}

	s.extenders = append(s.extenders, ext)
	if ext.IsNotifier() {
		ext.SetNotifyChannel(s.ctx, s.extCh)
	}
	s.retrospectiveStatus[ext.Name()] = make(map[string]bool)

	return nil
}

func (s *Scheduler) GetNodesDict() ([]*node.Node, error) {
	if s.root == nil {
		return nil, fmt.Errorf("Graph is empty")
	}

	var nodes []*node.Node
	var dfsErr error

	err := graph.DFS(s.dag, s.root.GetName(), func(name string) bool {
		vertex, props, err := s.dag.VertexWithProperties(name)
		if err != nil {
			dfsErr = fmt.Errorf("Couldn't get %s vertex from the graph: %v", name, err)
			return true
		}

		if props.Attributes["type"] == string(node.ModuleType) {
			nodes = append(nodes, vertex)
		}

		return false
	})

	if dfsErr != nil {
		return nodes, dfsErr
	}

	return nodes, err
}

func (s *Scheduler) PrintSummary() (map[string]bool, error) {
	result := make(map[string]bool, 0)
	vertices, err := s.GetNodesDict()
	if err != nil {
		return result, err
	}

	for _, vertex := range vertices {
		if vertex.GetType() == node.ModuleType {
			result[fmt.Sprintf("%s/%s", vertex.GetName(), vertex.GetUpdatedBy())] = vertex.GetState()
		}
	}
	return result, nil
}

func moduleSortFunc(m1, m2 string) bool {
	return m1 < m2
}

func (s *Scheduler) IsModuleEnabled(moduleName string) bool {
	vertex, err := s.dag.Vertex(moduleName)
	if err != nil {
		return false
	}

	return vertex.GetState()
}

func (s *Scheduler) GetEnabledModuleNames() ([]string, error) {
	s.l.Lock()
	defer s.l.Unlock()
	if s.enabledModules != nil {
		return *s.enabledModules, nil
	}

	enabledModules := make([]string, 0)
	nodeNames, err := graph.StableTopologicalSort(s.dag, moduleSortFunc)
	if err != nil {
		return nil, fmt.Errorf("Couldn't get the graph topological sorted view: %v", err)
	}

	for _, name := range nodeNames {
		if vertex, props, err := s.dag.VertexWithProperties(name); err == nil {
			if props.Attributes["type"] == string(node.ModuleType) && vertex.GetState() {
				enabledModules = append(enabledModules, name)
			}
		} else {
			return enabledModules, fmt.Errorf("Couldn't get %s vertex from the graph: %v", name, err)
		}
	}

	s.enabledModules = &enabledModules
	return enabledModules, nil
}

func (s *Scheduler) StateChanged(extName extenders.ExtenderName, moduleName string) (bool, error) {
	var (
		newStatus        bool
		previousStatus   bool
		cumulativeStatus bool
		extenderId       = -1
	)

	s.l.Lock()
	defer s.l.Unlock()
	vertex, props, err := s.dag.VertexWithProperties(moduleName)
	if err != nil {
		return false, fmt.Errorf("Couldn't get %s vertex from the graph: %v", moduleName, err)
	}

	if props.Attributes["type"] != string(node.ModuleType) {
		return false, fmt.Errorf("Vertex %s isn't of module type", moduleName)
	}

	for i, ex := range s.extenders {
		if ex.Name() == extName {
			extenderId = i
			break
		}

		if exStatus, has := s.retrospectiveStatus[ex.Name()][moduleName]; has && cumulativeStatus != exStatus {
			cumulativeStatus = exStatus
		}
	}

	if extenderId < 0 {
		return false, fmt.Errorf("Extender %s not found", extName)
	}

	extenderStatus, err := s.extenders[extenderId].Filter(vertex.GetModule())
	if err != nil {
		return false, err
	}

	if extenderStatus != nil && cumulativeStatus != *extenderStatus {
		newStatus = *extenderStatus
	} else {
		newStatus = cumulativeStatus
	}

	if retroStatus, has := s.retrospectiveStatus[extName][moduleName]; has && cumulativeStatus != retroStatus {
		previousStatus = retroStatus
	} else {
		previousStatus = cumulativeStatus
	}

	// retro status is empty
	return newStatus != previousStatus, nil
}

func (s *Scheduler) UpdateAndApplyNewState() (map[string]bool, error) {
	diff := make(map[string]bool, 0)
	enabledModules := make([]string, 0)
	s.l.Lock()
	defer s.l.Unlock()
	names, err := graph.StableTopologicalSort(s.dag, moduleSortFunc)
	if err != nil {
		return diff, err
	}

	for _, name := range names {
		vertex, props, err := s.dag.VertexWithProperties(name)
		if err != nil {
			return diff, fmt.Errorf("Couldn't get %s vertex from the graph: %v", name, err)
		}

		if props.Attributes["type"] == string(node.ModuleType) {
			previousState := vertex.GetState()
			vertex.SetState(false)
			vertex.SetUpdatedBy("")

			for _, ex := range s.extenders {
				// if ex is a shutter and the module is already disabled - there's no sense in running the extender
				if ex.IsShutter() && !vertex.GetState() {
					continue
				}

				moduleStatus, err := ex.Filter(vertex.GetModule())
				if err != nil {
					return diff, err
				}

				if moduleStatus != nil {
					if ex.IsShutter() {
						if !*moduleStatus && vertex.GetState() {
							vertex.SetState(*moduleStatus)
							vertex.SetUpdatedBy(string(ex.Name()))
							s.retrospectiveStatus[ex.Name()][vertex.GetName()] = vertex.GetState()
						}
						break
					}

					vertex.SetState(*moduleStatus)
					vertex.SetUpdatedBy(string(ex.Name()))
					s.retrospectiveStatus[ex.Name()][vertex.GetName()] = vertex.GetState()
				} else {
					delete(s.retrospectiveStatus[ex.Name()], vertex.GetName())
				}
			}

			if previousState != vertex.GetState() {
				diff[vertex.GetName()] = vertex.GetState()
			}

			if vertex.GetState() {
				enabledModules = append(enabledModules, name)
			}
		}
	}

	for i := range s.extenders {
		s.extenders[i].Reset()
	}

	s.enabledModules = &enabledModules

	s.printGraph()
	return diff, nil
}

func (s *Scheduler) DumpExtender(name extenders.ExtenderName) map[string]bool {
	for _, ex := range s.extenders {
		if ex.Name() == name {
			return ex.Dump()
		}
	}

	return nil
}
