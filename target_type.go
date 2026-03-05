package rediver

import (
	"github.com/califio/rediver-sdk-go/internal/api"
)

// TargetType is a type alias for the generated API AssetTypes enum.
type TargetType = api.AssetTypes

const (
	TargetTypeASN        = api.AssetTypesAsn
	TargetTypeIP         = api.AssetTypesIp
	TargetTypeSubnet     = api.AssetTypesSubnet
	TargetTypeDomain     = api.AssetTypesSubdomain
	TargetTypeRootDomain = api.AssetTypesRootDomain
	TargetTypeService    = api.AssetTypesService
	TargetTypeRepository = api.AssetTypesRepository
)
