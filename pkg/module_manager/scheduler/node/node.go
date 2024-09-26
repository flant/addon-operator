package node

import (
	"strconv"
)

type ModuleInterface interface {
	RunEnabledScript(string, []string, map[string]string) (bool, error)
	GetName() string
	GetOrder() uint32
	GetPath() string
}

type NodeType string

type NodeWeight uint32

func (weight NodeWeight) String() string {
	return strconv.FormatUint(uint64(weight), 10)
}

func (weight NodeWeight) Int() int {
	return int(weight)
}

type Node struct {
	name      string
	weight    NodeWeight
	typ       NodeType
	enabled   bool
	updatedBy string
	module    ModuleInterface
}

type NodeWeightRange []NodeWeight

func (r NodeWeightRange) Len() int {
	return len(r)
}

func (r NodeWeightRange) Less(i, j int) bool {
	return r[i] < r[j]
}

func (r NodeWeightRange) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}

const (
	ModuleType NodeType = "module"
	WeightType NodeType = "weight"
)

func NewNode() *Node {
	return &Node{}
}

func (n *Node) WithName(name string) *Node {
	n.name = name
	return n
}

func (n *Node) WithWeight(order uint32) *Node {
	n.weight = NodeWeight(order)
	return n
}

func (n *Node) WithModule(module ModuleInterface) *Node {
	n.module = module
	return n
}

func (n *Node) WithType(typ NodeType) *Node {
	n.typ = typ
	return n
}

func (n Node) GetName() string {
	return n.name
}

func (n Node) GetWeight() NodeWeight {
	return n.weight
}

func (n Node) GetState() bool {
	return n.enabled
}

func (n Node) GetType() NodeType {
	return n.typ
}

func (n Node) GetModule() ModuleInterface {
	return n.module
}

func (n Node) GetUpdatedBy() string {
	return n.updatedBy
}

func (n *Node) SetState(enabled bool) {
	n.enabled = enabled
}

func (n *Node) SetUpdatedBy(updatedBy string) {
	n.updatedBy = updatedBy
}
