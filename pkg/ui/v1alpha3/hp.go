package v1alpha3

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	commonv1alpha3 "github.com/kubeflow/katib/pkg/apis/controller/common/v1alpha3"
	trialsv1alpha3 "github.com/kubeflow/katib/pkg/apis/controller/trials/v1alpha3"
	api_pb_v1alpha3 "github.com/kubeflow/katib/pkg/apis/manager/v1alpha3"
)

// FetchAllHPJobs gets experiments in all namespaces.
func (k *KatibUIHandler) FetchAllHPJobs(w http.ResponseWriter, r *http.Request) {
	// Use "" to get experiments in all namespaces.
	jobs, err := k.getExperimentList("", JobTypeHP)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response, err := json.Marshal(jobs)
	if err != nil {
		log.Printf("Marshal HP jobs failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(response)
}

// FetchHPJob gets experiment in specific namespace.
func (k *KatibUIHandler) FetchHPJob(w http.ResponseWriter, r *http.Request) {
	experimentName := r.URL.Query()["experimentName"][0]
	namespace := r.URL.Query()["namespace"][0]

	experiment, err := k.katibClient.GetExperiment(experimentName, namespace)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	response, err := json.Marshal(experiment)
	if err != nil {
		log.Printf("Marshal HP job failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(response)
}

func (k *KatibUIHandler) FetchHPJobInfo(w http.ResponseWriter, r *http.Request) {
	//enableCors(&w)
	experimentName := r.URL.Query()["experimentName"][0]
	namespace := r.URL.Query()["namespace"][0]

	conn, c := k.connectManager()
	defer conn.Close()

	resultText := "trialName,Status"
	experiment, err := k.katibClient.GetExperiment(experimentName, namespace)
	if err != nil {
		log.Printf("GetExperiment from HP job failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Got Experiment")
	metricsList := map[string]int{}
	metricsName := experiment.Spec.Objective.ObjectiveMetricName
	resultText += "," + metricsName
	metricsList[metricsName] = 0
	for i, m := range experiment.Spec.Objective.AdditionalMetricNames {
		resultText += "," + m
		metricsList[m] = i + 1
	}
	log.Printf("Got metrics names")
	paramList := map[string]int{}
	for i, p := range experiment.Spec.Parameters {
		resultText += "," + p.Name
		paramList[p.Name] = i + len(metricsList)
	}
	log.Printf("Got Parameters names")

	trialList, err := k.katibClient.GetTrialList(experimentName, namespace)
	if err != nil {
		log.Printf("GetTrialList from HP job failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Got Trial List")

	for _, t := range trialList.Items {
		succeeded := false
		for _, condition := range t.Status.Conditions {
			if condition.Type == trialsv1alpha3.TrialSucceeded &&
				condition.Status == corev1.ConditionTrue {
				succeeded = true
			}
		}
		var lastTrialCondition string

		// Take only the latest condition
		if len(t.Status.Conditions) > 0 {
			lastTrialCondition = string(t.Status.Conditions[len(t.Status.Conditions)-1].Type)
		}

		trialResText := make([]string, len(metricsList)+len(paramList))

		if succeeded {
			obsLogResp, err := c.GetObservationLog(
				context.Background(),
				&api_pb_v1alpha3.GetObservationLogRequest{
					TrialName: t.Name,
					StartTime: "",
					EndTime:   "",
				},
			)
			if err != nil {
				log.Printf("GetObservationLog from HP job failed: %v", err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			for _, m := range obsLogResp.ObservationLog.MetricLogs {
				if trialResText[metricsList[m.Metric.Name]] == "" {
					trialResText[metricsList[m.Metric.Name]] = m.Metric.Value
				} else {
					currentValue, _ := strconv.ParseFloat(m.Metric.Value, 64)
					bestValue, _ := strconv.ParseFloat(trialResText[metricsList[m.Metric.Name]], 64)
					if t.Spec.Objective.Type == commonv1alpha3.ObjectiveTypeMinimize && currentValue < bestValue {
						trialResText[metricsList[m.Metric.Name]] = m.Metric.Value
					} else if t.Spec.Objective.Type == commonv1alpha3.ObjectiveTypeMaximize && currentValue > bestValue {
						trialResText[metricsList[m.Metric.Name]] = m.Metric.Value
					}
				}
			}
		}
		for _, trialParam := range t.Spec.ParameterAssignments {
			trialResText[paramList[trialParam.Name]] = trialParam.Value
		}
		resultText += "\n" + t.Name + "," + lastTrialCondition + "," + strings.Join(trialResText, ",")
	}
	log.Printf("Logs parsed, results:\n %v", resultText)
	response, err := json.Marshal(resultText)
	if err != nil {
		log.Printf("Marshal result text for HP job failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(response)
}

// FetchHPJobTrialInfo returns all metrics for the HP Job Trial
func (k *KatibUIHandler) FetchHPJobTrialInfo(w http.ResponseWriter, r *http.Request) {
	//enableCors(&w)
	trialName := r.URL.Query()["trialName"][0]
	namespace := r.URL.Query()["namespace"][0]
	conn, c := k.connectManager()
	defer conn.Close()

	trial, err := k.katibClient.GetTrial(trialName, namespace)

	if err != nil {
		log.Printf("GetTrial from HP job failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}

	objectiveType := trial.Spec.Objective.Type

	// resultArray - array of arrays, where [i][0] - metricName, [i][1] - metricTime, [i][2] - metricValue
	var resultArray [][]string
	resultArray = append(resultArray, strings.Split("metricName,time,value", ","))
	obsLogResp, err := c.GetObservationLog(
		context.Background(),
		&api_pb_v1alpha3.GetObservationLogRequest{
			TrialName: trialName,
			StartTime: "",
			EndTime:   "",
		},
	)
	if err != nil {
		log.Printf("GetObservationLog failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// prevMetricTimeValue is the dict, where key = metric name,
	// value = array, where [0] - Last metric time, [1] - Best metric value for this time
	prevMetricTimeValue := make(map[string][]string)
	for _, m := range obsLogResp.ObservationLog.MetricLogs {
		parsedCurrentTime, _ := time.Parse(time.RFC3339Nano, m.TimeStamp)
		formatCurrentTime := parsedCurrentTime.Format("2006-01-02T15:04:05")
		if _, found := prevMetricTimeValue[m.Metric.Name]; !found {
			prevMetricTimeValue[m.Metric.Name] = []string{"", ""}

		}
		if formatCurrentTime == prevMetricTimeValue[m.Metric.Name][0] &&
			((objectiveType == commonv1alpha3.ObjectiveTypeMinimize &&
				m.Metric.Value < prevMetricTimeValue[m.Metric.Name][1]) ||
				(objectiveType == commonv1alpha3.ObjectiveTypeMaximize &&
					m.Metric.Value > prevMetricTimeValue[m.Metric.Name][1])) {

			prevMetricTimeValue[m.Metric.Name][1] = m.Metric.Value
			for i := len(resultArray) - 1; i >= 0; i-- {
				if resultArray[i][0] == m.Metric.Name {
					resultArray[i][2] = m.Metric.Value
					break
				}
			}
		} else if formatCurrentTime != prevMetricTimeValue[m.Metric.Name][0] {
			resultArray = append(resultArray, []string{m.Metric.Name, formatCurrentTime, m.Metric.Value})
			prevMetricTimeValue[m.Metric.Name][0] = formatCurrentTime
			prevMetricTimeValue[m.Metric.Name][1] = m.Metric.Value
		}
	}

	var resultText string
	for _, metric := range resultArray {
		resultText += strings.Join(metric, ",") + "\n"
	}

	response, err := json.Marshal(resultText)
	if err != nil {
		log.Printf("Marshal result text in Trial info failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Write(response)
}
