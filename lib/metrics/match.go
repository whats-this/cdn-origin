package metrics

import "strings"

// treeNode is used for domain whitelisting.
type treeNode struct {
	Leaf      bool
	Value     string
	FullValue string
	SubNodes  []*treeNode
}

func (t *treeNode) findOrCreateSubNode(v string) *treeNode {
	if t.SubNodes == nil {
		t.SubNodes = []*treeNode{}
	}

	for _, n := range t.SubNodes {
		if !n.Leaf && n.Value == v {
			return n
		}
	}
	node := &treeNode{Value: v}
	t.SubNodes = append(t.SubNodes, node)
	return node
}

func (t *treeNode) getMatch(s []string) string {
	if t.Leaf || len(t.SubNodes) == 0 || len(s) == 0 {
		return ""
	}

	for _, node := range t.SubNodes {
		if node.Value == "*" || node.Value == s[0] {
			if node.Leaf {
				return node.FullValue
			}
			if match := node.getMatch(s[1:]); match != "" {
				return match
			}
		}
	}

	return ""
}

func parseWhitelistSlice(hostnameWhitelist []string) *treeNode {
	// NOTE: using arrays because the provided order is important (maps would be a check if part in in map,
	// then check for *, which works but doesn't maintain order)
	tree := &treeNode{SubNodes: []*treeNode{}}
	for _, d := range hostnameWhitelist {
		split := strings.Split(d, ".")

		currentNode := tree
		for i, s := range split {
			currentNode = currentNode.findOrCreateSubNode(s)

			if i+1 == len(split) {
				currentNode.Leaf = true
				currentNode.FullValue = d
				continue
			}
			if currentNode.SubNodes == nil {
				currentNode.SubNodes = []*treeNode{}
			}
		}
	}
	return tree
}
