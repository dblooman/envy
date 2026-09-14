-- One canonical installation owns a metadata database, matching the global lease.
CREATE TABLE installation_profile (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 installation_id text NOT NULL,
 provider text NOT NULL CHECK (provider IN ('istio', 'cilium', 'linkerd'))
);
