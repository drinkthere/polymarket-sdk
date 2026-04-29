package positions

type ListRequest struct {
	User          string
	Redeemable    *bool
	Mergeable     *bool
	Limit         int
	Offset        int
	SizeThreshold string
}

type ListResponse struct {
	Positions []Position
}

type Position struct {
	Asset        string  `json:"asset"`
	ConditionID  string  `json:"conditionId"`
	Size         float64 `json:"size"`
	AvgPrice     float64 `json:"avgPrice"`
	Title        string  `json:"title"`
	Outcome      string  `json:"outcome"`
	Side         string  `json:"side"`
	NegativeRisk bool    `json:"negativeRisk"`
	OutcomeIndex int     `json:"outcomeIndex"`
}
