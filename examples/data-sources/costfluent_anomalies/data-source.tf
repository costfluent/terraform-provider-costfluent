# List detected anomalies
data "costfluent_anomalies" "current" {}

output "anomaly_count" {
  value = length(data.costfluent_anomalies.current.anomalies)
}

output "critical_anomalies" {
  value = [for a in data.costfluent_anomalies.current.anomalies : a if a.severity == "critical"]
}

# Use in alerting logic
output "has_unresolved_anomalies" {
  value = length([for a in data.costfluent_anomalies.current.anomalies : a if a.status == "new"]) > 0
}
