package material

// ImageAsset is an immutable crop of a rendered source PDF page. Data is private;
// public card JSON contains only metadata and an authorized API URL.
type ImageAsset struct {
	ID         string `json:"assetId"`
	Alt        string `json:"alt"`
	SourceName string `json:"sourceName"`
	SourcePage int    `json:"sourcePage"`
	SourceFile int    `json:"sourceFile"`
	Region     Region `json:"region"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Hash       string `json:"hash"`
	Data       []byte `json:"-"`
}
