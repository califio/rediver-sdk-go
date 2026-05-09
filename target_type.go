package rediver

import scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"

// TargetType is the scanner asset type (public SDK type — unchanged from v1).
type TargetType string

const (
	TargetTypeASN        TargetType = "Asn"
	TargetTypeIP         TargetType = "Ip"
	TargetTypeSubnet     TargetType = "Subnet"
	TargetTypeDomain     TargetType = "Subdomain"
	TargetTypeRootDomain TargetType = "RootDomain"
	TargetTypeService    TargetType = "Service"
	TargetTypeRepository TargetType = "Repository"
)

// toProtoAssetType converts an SDK TargetType to the proto AssetType enum.
func toProtoAssetType(t TargetType) scannerv1.AssetType {
	switch t {
	case TargetTypeASN:
		return scannerv1.AssetType_ASSET_TYPE_ASN
	case TargetTypeIP:
		return scannerv1.AssetType_ASSET_TYPE_IP
	case TargetTypeSubnet:
		return scannerv1.AssetType_ASSET_TYPE_SUBNET
	case TargetTypeDomain:
		return scannerv1.AssetType_ASSET_TYPE_SUBDOMAIN
	case TargetTypeRootDomain:
		return scannerv1.AssetType_ASSET_TYPE_ROOT_DOMAIN
	case TargetTypeService:
		return scannerv1.AssetType_ASSET_TYPE_SERVICE
	case TargetTypeRepository:
		return scannerv1.AssetType_ASSET_TYPE_REPOSITORY
	default:
		return scannerv1.AssetType_ASSET_TYPE_UNSPECIFIED
	}
}
