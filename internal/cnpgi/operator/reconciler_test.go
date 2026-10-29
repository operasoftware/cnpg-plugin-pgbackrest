/*
Copyright 2025, Opera Norway AS

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

package operator

import (
	"context"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

var _ = Describe("ReconcilerImplementation with concurrent replicas", func() {
	var reconciler ReconcilerImplementation
	cluster := &cnpgv1.Cluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster", Namespace: "default", UID: "uid"}}

	// Another replica creates the object between our Get and Create: the Get
	// finds nothing and the Create fails with AlreadyExists.
	BeforeEach(func() {
		scheme := runtime.NewScheme()
		Expect(cnpgv1.AddToScheme(scheme)).To(Succeed())
		Expect(rbacv1.AddToScheme(scheme)).To(Succeed())
		reconciler = ReconcilerImplementation{
			Client: fake.NewClientBuilder().WithScheme(scheme).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(_ context.Context, _ client.WithWatch, key client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
					return apierrs.NewNotFound(schema.GroupResource{Group: "rbac.authorization.k8s.io"}, key.Name)
				},
				Create: func(_ context.Context, _ client.WithWatch, obj client.Object, _ ...client.CreateOption) error {
					return apierrs.NewAlreadyExists(schema.GroupResource{Group: "rbac.authorization.k8s.io"}, obj.GetName())
				},
			}).Build(),
		}
	})

	It("treats a Role created by another replica as success", func(ctx SpecContext) {
		Expect(reconciler.ensureRole(ctx, cluster, nil)).To(Succeed())
	})

	It("treats a RoleBinding created by another replica as success", func(ctx SpecContext) {
		Expect(reconciler.ensureRoleBinding(ctx, cluster)).To(Succeed())
	})
})
