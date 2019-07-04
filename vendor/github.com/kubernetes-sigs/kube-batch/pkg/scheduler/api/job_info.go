/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package api

import (
	"fmt"
	"sort"
	"strings"

	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"
)

// TaskID is UID type for Task
type TaskID types.UID

// TaskInfo will have all infos about the task
type TaskInfo struct {
	UID TaskID
	Job JobID

	Name      string
	Namespace string

	// Resreq is the resource that used when task running.
	Resreq *Resource
	// InitResreq is the resource that used to launch a task.
	InitResreq *Resource

	NodeName    string
	Status      TaskStatus
	Priority    int32
	VolumeReady bool

	Pod *v1.Pod
}

func getJobID(pod *v1.Pod) JobID {
	if len(pod.Annotations) != 0 {
		if gn, found := pod.Annotations[v1alpha1.GroupNameAnnotationKey]; found && len(gn) != 0 {
			// Make sure Pod and PodGroup belong to the same namespace.
			jobID := fmt.Sprintf("%s/%s", pod.Namespace, gn)
			return JobID(jobID)
		}
	}

	return ""
}

// NewTaskInfo creates new taskInfo object for a Pod
func NewTaskInfo(pod *v1.Pod) *TaskInfo {
	req := GetPodResourceWithoutInitContainers(pod)
	initResreq := GetPodResourceRequest(pod)

	jobID := getJobID(pod)

	ti := &TaskInfo{
		UID:        TaskID(pod.UID),
		Job:        jobID,
		Name:       pod.Name,
		Namespace:  pod.Namespace,
		NodeName:   pod.Spec.NodeName,
		Status:     getTaskStatus(pod),
		Priority:   1,
		Pod:        pod,
		Resreq:     req,
		InitResreq: initResreq,
	}

	if pod.Spec.Priority != nil {
		ti.Priority = *pod.Spec.Priority
	}

	return ti
}

// Clone is used for cloning a task
func (ti *TaskInfo) Clone() *TaskInfo {
	return &TaskInfo{
		UID:         ti.UID,
		Job:         ti.Job,
		Name:        ti.Name,
		Namespace:   ti.Namespace,
		NodeName:    ti.NodeName,
		Status:      ti.Status,
		Priority:    ti.Priority,
		Pod:         ti.Pod,
		Resreq:      ti.Resreq.Clone(),
		InitResreq:  ti.InitResreq.Clone(),
		VolumeReady: ti.VolumeReady,
	}
}

// String returns the taskInfo details in a string
func (ti TaskInfo) String() string {
	return fmt.Sprintf("Task (%v:%v/%v): job %v, status %v, pri %v, resreq %v",
		ti.UID, ti.Namespace, ti.Name, ti.Job, ti.Status, ti.Priority, ti.Resreq)
}

// JobID is the type of JobInfo's ID.
type JobID types.UID

type tasksMap map[TaskID]*TaskInfo

// NodeResourceMap stores resource in a node
type NodeResourceMap map[string]*Resource

// JobInfo will have all info of a Job
type JobInfo struct {
	UID JobID

	Name      string
	Namespace string

	Queue QueueID

	Priority int32

	NodeSelector map[string]string
	MinAvailable int32

	NodesFitDelta NodeResourceMap

	JobFitErrors   string
	NodesFitErrors map[TaskID]*FitErrors

	// All tasks of the Job.
	TaskStatusIndex map[TaskStatus]tasksMap
	Tasks           tasksMap

	Allocated    *Resource
	TotalRequest *Resource

	CreationTimestamp metav1.Time
	PodGroup          *v1alpha1.PodGroup

	// TODO(k82cn): keep backward compatibility, removed it when v1alpha1 finalized.
	PDB *policyv1.PodDisruptionBudget
}

// NewJobInfo creates a new jobInfo for set of tasks
func NewJobInfo(uid JobID, tasks ...*TaskInfo) *JobInfo {
	job := &JobInfo{
		UID: uid,

		MinAvailable:  0,
		NodeSelector:  make(map[string]string),
		NodesFitDelta: make(NodeResourceMap),
		Allocated:     EmptyResource(),
		TotalRequest:  EmptyResource(),

		NodesFitErrors: make(map[TaskID]*FitErrors),

		TaskStatusIndex: map[TaskStatus]tasksMap{},
		Tasks:           tasksMap{},
	}

	for _, task := range tasks {
		job.AddTaskInfo(task)
	}

	return job
}

// UnsetPodGroup removes podGroup details from a job
func (ji *JobInfo) UnsetPodGroup() {
	ji.PodGroup = nil
}

// SetPodGroup sets podGroup details to a job
func (ji *JobInfo) SetPodGroup(pg *v1alpha1.PodGroup) {
	ji.Name = pg.Name
	ji.Namespace = pg.Namespace
	ji.MinAvailable = pg.Spec.MinMember
	ji.Queue = QueueID(pg.Spec.Queue)
	ji.CreationTimestamp = pg.GetCreationTimestamp()

	ji.PodGroup = pg
}

// SetPDB sets PDB to a job
func (ji *JobInfo) SetPDB(pdb *policyv1.PodDisruptionBudget) {
	ji.Name = pdb.Name
	ji.MinAvailable = pdb.Spec.MinAvailable.IntVal
	ji.Namespace = pdb.Namespace

	ji.CreationTimestamp = pdb.GetCreationTimestamp()
	ji.PDB = pdb
}

// UnsetPDB removes PDB info of a job
func (ji *JobInfo) UnsetPDB() {
	ji.PDB = nil
}

// GetTasks gets all tasks with the taskStatus
func (ji *JobInfo) GetTasks(statuses ...TaskStatus) []*TaskInfo {
	var res []*TaskInfo

	for _, status := range statuses {
		if tasks, found := ji.TaskStatusIndex[status]; found {
			for _, task := range tasks {
				res = append(res, task.Clone())
			}
		}
	}

	return res
}

func (ji *JobInfo) addTaskIndex(ti *TaskInfo) {
	if _, found := ji.TaskStatusIndex[ti.Status]; !found {
		ji.TaskStatusIndex[ti.Status] = tasksMap{}
	}

	ji.TaskStatusIndex[ti.Status][ti.UID] = ti
}

// AddTaskInfo is used to add a task to a job
func (ji *JobInfo) AddTaskInfo(ti *TaskInfo) {
	ji.Tasks[ti.UID] = ti
	ji.addTaskIndex(ti)

	ji.TotalRequest.Add(ti.Resreq)

	if AllocatedStatus(ti.Status) {
		ji.Allocated.Add(ti.Resreq)
	}
}

// UpdateTaskStatus is used to update task's status in a job
func (ji *JobInfo) UpdateTaskStatus(task *TaskInfo, status TaskStatus) error {
	if err := validateStatusUpdate(task.Status, status); err != nil {
		return err
	}

	// Remove the task from the task list firstly
	ji.DeleteTaskInfo(task)

	// Update task's status to the target status
	task.Status = status
	ji.AddTaskInfo(task)

	return nil
}

func (ji *JobInfo) deleteTaskIndex(ti *TaskInfo) {
	if tasks, found := ji.TaskStatusIndex[ti.Status]; found {
		delete(tasks, ti.UID)

		if len(tasks) == 0 {
			delete(ji.TaskStatusIndex, ti.Status)
		}
	}
}

// DeleteTaskInfo is used to delete a task from a job
func (ji *JobInfo) DeleteTaskInfo(ti *TaskInfo) error {
	if task, found := ji.Tasks[ti.UID]; found {
		ji.TotalRequest.Sub(task.Resreq)

		if AllocatedStatus(task.Status) {
			ji.Allocated.Sub(task.Resreq)
		}

		delete(ji.Tasks, task.UID)

		ji.deleteTaskIndex(task)
		return nil
	}

	return fmt.Errorf("failed to find task <%v/%v> in job <%v/%v>",
		ti.Namespace, ti.Name, ji.Namespace, ji.Name)
}

// Clone is used to clone a jobInfo object
func (ji *JobInfo) Clone() *JobInfo {
	info := &JobInfo{
		UID:       ji.UID,
		Name:      ji.Name,
		Namespace: ji.Namespace,
		Queue:     ji.Queue,
		Priority:  ji.Priority,

		MinAvailable:  ji.MinAvailable,
		NodeSelector:  map[string]string{},
		Allocated:     EmptyResource(),
		TotalRequest:  EmptyResource(),
		NodesFitDelta: make(NodeResourceMap),

		NodesFitErrors: make(map[TaskID]*FitErrors),

		PDB:      ji.PDB,
		PodGroup: ji.PodGroup.DeepCopy(),

		TaskStatusIndex: map[TaskStatus]tasksMap{},
		Tasks:           tasksMap{},
	}

	ji.CreationTimestamp.DeepCopyInto(&info.CreationTimestamp)

	for k, v := range ji.NodeSelector {
		info.NodeSelector[k] = v
	}

	for _, task := range ji.Tasks {
		info.AddTaskInfo(task.Clone())
	}

	return info
}

// String returns a jobInfo object in string format
func (ji JobInfo) String() string {
	res := ""

	i := 0
	for _, task := range ji.Tasks {
		res = res + fmt.Sprintf("\n\t %d: %v", i, task)
		i++
	}

	return fmt.Sprintf("Job (%v): namespace %v (%v), name %v, minAvailable %d, podGroup %+v",
		ji.UID, ji.Namespace, ji.Queue, ji.Name, ji.MinAvailable, ji.PodGroup) + res
}

// FitError returns detailed information on why a job's task failed to fit on
// each available node
func (ji *JobInfo) FitError() string {
	reasons := make(map[string]int)
	for status, taskMap := range ji.TaskStatusIndex {
		reasons[fmt.Sprintf("%s", status)] += len(taskMap)
	}
	reasons["minAvailable"] = int(ji.MinAvailable)

	sortReasonsHistogram := func() []string {
		reasonStrings := []string{}
		for k, v := range reasons {
			reasonStrings = append(reasonStrings, fmt.Sprintf("%v %v", v, k))
		}
		sort.Strings(reasonStrings)
		return reasonStrings
	}
	reasonMsg := fmt.Sprintf("job is not ready, %v.", strings.Join(sortReasonsHistogram(), ", "))
	return reasonMsg
}

// ReadyTaskNum returns the number of tasks that are ready.
func (ji *JobInfo) ReadyTaskNum() int32 {
	occupid := 0
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded {
			occupid = occupid + len(tasks)
		}
	}

	return int32(occupid)
}

// WaitingTaskNum returns the number of tasks that are pipelined.
func (ji *JobInfo) WaitingTaskNum() int32 {
	occupid := 0
	for status, tasks := range ji.TaskStatusIndex {
		if status == Pipelined {
			occupid = occupid + len(tasks)
		}
	}

	return int32(occupid)
}

// ValidTaskNum returns the number of tasks that are valid.
func (ji *JobInfo) ValidTaskNum() int32 {
	occupied := 0
	for status, tasks := range ji.TaskStatusIndex {
		if AllocatedStatus(status) ||
			status == Succeeded ||
			status == Pipelined ||
			status == Pending {
			occupied = occupied + len(tasks)
		}
	}

	return int32(occupied)
}

// Ready returns whether job is ready for run
func (ji *JobInfo) Ready() bool {
	occupied := ji.ReadyTaskNum()

	return occupied >= ji.MinAvailable
}

// Pipelined returns whether the number of ready and pipelined task is enough
func (ji *JobInfo) Pipelined() bool {
	occupied := ji.WaitingTaskNum() + ji.ReadyTaskNum()

	return occupied >= ji.MinAvailable
}
