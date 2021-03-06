/*
File Name:  Network Detection.go
Copyright:  2021 Peernet Foundation s.r.o.
Author:     Peter Kleissner
*/

package core

import (
	"log"
	"net"
	"strings"
	"time"
)

// FindInterfaceByIP finds an interface based on the IP. The IP must be available at the interface.
func FindInterfaceByIP(ip net.IP) (iface *net.Interface, ipnet *net.IPNet) {
	interfaceList, err := net.Interfaces()
	if err != nil {
		return nil, nil
	}

	// iterate through all interfaces
	for _, ifaceSingle := range interfaceList {
		addresses, err := ifaceSingle.Addrs()
		if err != nil {
			continue
		}

		// iterate through all IPs of the interfaces
		for _, address := range addresses {
			addressIP := address.(*net.IPNet).IP

			if addressIP.Equal(ip) {
				return &ifaceSingle, address.(*net.IPNet)
			}
		}
	}

	return nil, nil
}

// NetworkListIPs returns a list of all IPs
func NetworkListIPs() (IPs []net.IP, err error) {

	interfaceList, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	// iterate through all interfaces
	for _, ifaceSingle := range interfaceList {
		addresses, err := ifaceSingle.Addrs()
		if err != nil {
			continue
		}

		// iterate through all IPs of the interfaces
		for _, address := range addresses {
			addressIP := address.(*net.IPNet).IP
			IPs = append(IPs, addressIP)
		}
	}

	return IPs, nil
}

// IsIPv4 checks if an IP address is IPv4
func IsIPv4(IP net.IP) bool {
	return IP.To4() != nil
}

// IsIPv6 checks if an IP address is IPv6
func IsIPv6(IP net.IP) bool {
	return IP.To4() == nil && IP.To16() != nil
}

// IsNetworkErrorFatal checks if a network error indicates a broken connection.
// Not every network error indicates a broken connection. This function prevents from over-dropping connections.
func IsNetworkErrorFatal(err error) bool {
	if err == nil {
		return false
	}

	// Windows: A common error when the network adapter is disabled is "wsasendto: The requested address is not valid in its context".
	if strings.Contains(err.Error(), "requested address is not valid in its context") {
		return true
	}

	return false
}

// changeMonitorFrequency is the frequency in seconds to check for a network change
const changeMonitorFrequency = 10

// networkChangeMonitor() monitors for network changes to act accordingly
func networkChangeMonitor() {
	// If manual IPs are entered, no need for monitoring for any network changes.
	if len(config.Listen) > 0 {
		return
	}

	for {
		time.Sleep(time.Second * changeMonitorFrequency)

		interfaceList, err := net.Interfaces()
		if err != nil {
			log.Printf("networkChangeMonitor enumerating network adapters failed: %s\n", err.Error())
			continue
		}

		ifacesNew := make(map[string][]net.Addr)

		for _, iface := range interfaceList {
			addressesNew, err := iface.Addrs()
			if err != nil {
				log.Printf("initNetwork error enumerating IPs for network adapter '%s': %s\n", iface.Name, err.Error())
				continue
			}
			ifacesNew[iface.Name] = addressesNew

			// was the interface added?
			addressesExist, ok := ifacesExist[iface.Name]
			if !ok {
				networkChangeInterfaceNew(iface, addressesNew)
			} else {
				// new IPs added for this interface?
				for _, addr := range addressesNew {
					exists := false
					for _, exist := range addressesExist {
						if exist.String() == addr.String() {
							exists = true
							break
						}
					}

					if !exists {
						networkChangeIPNew(iface, addr)
					}
				}

				// were IPs removed from this interface
				for _, exist := range addressesExist {
					removed := true
					for _, addr := range addressesNew {
						if exist.String() == addr.String() {
							removed = false
							break
						}
					}

					if removed {
						networkChangeIPRemove(iface, exist)
					}
				}
			}
		}

		// was an existing interface removed?
		for ifaceExist, addressesExist := range ifacesExist {
			if _, ok := ifacesNew[ifaceExist]; !ok {
				networkChangeInterfaceRemove(ifaceExist, addressesExist)
			}
		}

		ifacesExist = ifacesNew
	}
}

// networkChangeInterfaceNew is called when a new interface is detected
func networkChangeInterfaceNew(iface net.Interface, addresses []net.Addr) {
	log.Printf("networkChangeInterfaceNew new interface '%s' (%d IPs)\n", iface.Name, len(addresses))

	networkStart(iface, addresses)
}

// networkChangeInterfaceRemove is called when an existing interface is removed
func networkChangeInterfaceRemove(iface string, addresses []net.Addr) {
	networksMutex.RLock()
	defer networksMutex.RUnlock()

	log.Printf("networkChangeInterfaceRemove removing interface '%s' (%d IPs)\n", iface, len(addresses))

	for n, network := range networks6 {
		if network.iface != nil && network.iface.Name == iface {
			network.Terminate()

			// remove from list
			networksNew := networks6[:n]
			if n < len(networks6)-1 {
				networksNew = append(networksNew, networks6[n+1:]...)
			}
			networks6 = networksNew
		}
	}

	for n, network := range networks4 {
		if network.iface != nil && network.iface.Name == iface {
			network.Terminate()

			// remove from list
			networksNew := networks4[:n]
			if n < len(networks4)-1 {
				networksNew = append(networksNew, networks4[n+1:]...)
			}
			networks4 = networksNew
		}
	}
}

// networkChangeIPNew is called when an existing interface lists a new IP
func networkChangeIPNew(iface net.Interface, address net.Addr) {
	log.Printf("networkChangeIPNew new interface '%s' IP %s\n", iface.Name, address.String())

	networkStart(iface, []net.Addr{address})
}

// networkChangeIPRemove is called when an existing interface removes an IP
func networkChangeIPRemove(iface net.Interface, address net.Addr) {
	networksMutex.RLock()
	defer networksMutex.RUnlock()

	log.Printf("networkChangeIPRemove remove interface '%s' IP %s\n", iface.Name, address.String())

	for n, network := range networks6 {
		if network.address.IP.Equal(address.(*net.IPNet).IP) {
			network.Terminate()

			// remove from list
			networksNew := networks6[:n]
			if n < len(networks6)-1 {
				networksNew = append(networksNew, networks6[n+1:]...)
			}
			networks6 = networksNew
		}
	}

	for n, network := range networks4 {
		if network.address.IP.Equal(address.(*net.IPNet).IP) {
			network.Terminate()

			// remove from list
			networksNew := networks4[:n]
			if n < len(networks4)-1 {
				networksNew = append(networksNew, networks4[n+1:]...)
			}
			networks4 = networksNew
		}
	}
}
