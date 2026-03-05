// Package utils provides system utilities for machine info and network operations.
package utils

import "net"

// GetIPAddress returns the first non-loopback IPv4 address.
func GetIPAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "unknown"
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "unknown"
}
