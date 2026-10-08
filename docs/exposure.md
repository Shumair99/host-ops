# Exposure, ownership and orphan candidates

Security observations include configured runtime user, privileged execution, Docker socket mounts, memory limits, writable root and image version references. Host socket addresses and Docker publications are separate observations. Non-loopback publications indicate potential exposure; firewall rules and genuine internet reachability are not inferred.

Ownership links registered sites to container, domain, upstream and configured application port. Unregistered running containers are unmanaged inventory, not orphans. Unregistered stopped/dead containers are possible orphan candidates only: dependencies, routes, volumes and recovery needs may still exist. This version does not infer that images, volumes, directories or DNS records are safe to remove, and never deletes resources.
